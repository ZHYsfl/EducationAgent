# EducationAgent —— 异步双 Agent 具身系统（Voice Agent + Arm Agent）

<p align="center">
  <b>对机械臂说话，任务自动执行</b><br>
  <i>Voice Agent 前台全双工语音交互，Arm Agent 后台操作机械臂真机 —— 两条消息队列异步解耦</i>
</p>

> 本仓库前身为 **VoxFlow**（全双工语音 PPT 生成助手）。当前版本复用其验证过的异步架构与语音链路，把后台的 PPT Agent 替换为操作机械臂的 **Arm Agent**。系统设计文档见 `../async_dual_agent_system_design.md`，工具网关契约见 `../api_of_embodied_tools.md` / `../api_of_voice_tools.md`，微调数据方案见 `../finetuning_of_arm_agent.md` / `../finetuning_of_voice_agent.md`。

---

## 系统总览

人对机械臂说话即可完成任务下发与变更：Voice Agent 与人实时语音交互、理解意图、下发任务并转述结果；Arm Agent 在后台调用具身工具链执行物理任务；两个 agent 通过两条 FIFO 消息队列异步解耦，互不阻塞。

```
  人 ⇄ 麦克风 → 前端 VAD → STT → ┌────────────────┐ → 前端播放器 ⇄ 人
                            │  Voice Agent   │
                            │ (Qwen3-4B 微调) │
                            └───┬────────▲───┘
        send_to_arm_agent ──────┘        └────── get_message_from_arm_agent
                │                            ▲
   message_from_voice_agent_queue   message_from_arm_agent_queue
                │                            ▲
 get_message_from_voice_agent ────┐          └────── send_to_voice_agent
                            └───┬──┴────────┐
                            │   Arm Agent    │
                            │ (Qwen3-4B 微调) │
                            └───┬───────────┘
                                │ RESTful 调用具身工具网关（127.0.0.1:8000）
                                ▼
   get_current_coordinates / move_to_coordinates / grab_the_block / release_the_block
                                │
                                ▼
                          机械臂真机 + 视觉摄像头
```

### 核心机制

- **人优先原则**：人的语音交互永远不被后台任务阻塞；后台消息只能通过「状态栏感知 + 主动消费」进入上下文，绝不插队打断当前推理。
- **`<queue_status>` 状态栏**（两侧统一为独立的 role=user 消息；tool response、user input、状态栏同时存在时顺序固定为 tool response → user input → 状态栏）：
  - Voice 侧：每条人类 user 消息之后紧跟一条 `<queue_status>empty/not empty</queue_status>` 状态栏消息（反映 arm→voice 队列），`not empty` 时调用 `get_message_from_arm_agent` 主动消费并语音转述。
  - Arm 侧：忙碌时**每条工具结果之后**追加一条 `<queue_status>` 状态栏消息，`not empty` 时调用 `get_message_from_voice_agent` 主动消费新指令（改颜色/改位置/取消）；空闲时由运行时自动消费队列，新任务由此进入上下文（此时不追加状态栏——队列刚排空恒为 empty）。
- **Qwen3 原生 `<tool_call>` 格式**：两个微调模型均以 Qwen3 chat template 原生格式 `<tool_call>\n{"name": "...", "arguments": {...}}\n</tool_call>` 输出工具调用（与微调数据契约一致），编排层负责解析执行。
- **role=tool 单条回写**：工具结果以一条 role=tool 消息写回上下文，chat template 渲染时进入 user 侧的 `<tool_response>` 块，与 Qwen3 原生多轮工具对话格式完全一致。
- **全双工语音链路**：浏览器前端 VAD（含回声消除）+ 前端 TTS 播放器 + 后端 ASR + 打断检测小模型；打断以 `</interrupted>` 标记截断并重组上下文。

## 角色与工具

| 角色 | 基座 | 部署 | 工具 |
| --- | --- | --- | --- |
| Voice Agent | Qwen3-4B-Instruct-2507 + QLoRA | RTX 3090 #1（vLLM :8001） | `send_to_arm_agent`、`get_message_from_arm_agent` |
| Arm Agent | Qwen3-4B-Instruct-2507 + QLoRA | RTX 3090 #2（vLLM :8004） | `get_current_coordinates`、`move_to_coordinates`、`grab_the_block`、`release_the_block`、`send_to_voice_agent`、`get_message_from_voice_agent` |
| 打断检测 | Qwen3-0.6B + LoRA | 同 3090 #1（:8003） | — |
| ASR | Qwen3-ASR | 同 3090 #1（:8002） | — |
| 具身工具网关 | — | :8000（RESTful，见 `api_of_embodied_tools.md`） | 机械臂真机 + 视觉摄像头 |

Arm Agent 的 4 个具身工具通过 RESTful 网关调用；2 个通信工具直接操作编排层持有的两条队列（队列是系统级共享状态，见设计文档 §4.1）。

## 快速开始

### 环境要求

- Go 1.23+、Node.js 18+、Python 3.10+（训练）
- 2 × RTX 3090 24GB（一卡一个 agent 的微调与推理）

### 1. 配置环境变量

```bash
cp implementation/.env.example implementation/.env
```

```env
# Voice Agent LLM（vLLM，3090 #1）
VOICE_LLM_BASE_URL=http://127.0.0.1:8001/v1
VOICE_LLM_MODEL=voice-agent
VOICE_LLM_API_KEY=dummy

# 打断检测（注意：8000 已保留给具身工具网关）
INTERRUPT_LLM_BASE_URL=http://127.0.0.1:8003/v1
INTERRUPT_LLM_MODEL=interrupt-detection
INTERRUPT_LLM_API_KEY=dummy

# Arm Agent LLM（vLLM，3090 #2）
ARM_LLM_BASE_URL=http://127.0.0.1:8004/v1
ARM_LLM_MODEL=arm-agent
ARM_LLM_API_KEY=dummy

# 具身工具 RESTful 网关（api_of_embodied_tools.md，固定 8000）
ARM_GATEWAY_BASE_URL=http://127.0.0.1:8000

# ASR
ASR_OPENAI_BASE_URL=http://127.0.0.1:8002/v1
ASR_MODEL_ID=/root/autodl-tmp/asr
ASR_API_KEY=EMPTY
```

### 2. 启动后端 / 前端 / 模型服务

```bash
cd implementation && go mod download && go run ./server   # Go 后端 :8080
cd implementation/frontend && npm install && npm run dev  # 前端 :5173
bash implementation/start_dual_voice_asr.sh               # 单卡三路 vLLM：voice:8001 / interrupt:8003 / asr:8002
# 另需：3090 #2 上启动 arm-agent vLLM（:8004），以及具身工具网关（:8000）
```

### 3. 运行测试

```bash
cd implementation && go test ./...
cd tool_calling_go && go test ./...
```

## 项目结构

```
EducationAgent/
├── implementation/                 # 主实现（Go 后端 + React 前端）
│   ├── frontend/                   # React 18 + Vite + Zustand SPA
│   ├── internal/
│   │   ├── handler/                # Gin HTTP / SSE 路由（voice_turn / voice / arm_stream）
│   │   ├── service/
│   │   │   ├── voice_agent_service.go  # Voice Agent 编排（流式 <tool_call> 解析 + 两轮推理）
│   │   │   ├── arm_service.go          # Arm Agent 运行时（<queue_status> 注入 + 工具链执行）
│   │   │   ├── interrupt_service.go    # 打断检测
│   │   │   └── asr_service.go          # 语音识别
│   │   ├── state/                  # AppState（双向队列/双历史/日志广播）+ AgentRuntime
│   │   ├── toolcalling/            # LLM Agent 框架（嵌入版）+ Qwen3 原生 <tool_call> 解析
│   │   ├── voiceagent/             # Voice 侧 <tool_call> 解析与执行
│   │   └── tools/                  # arm_gateway.go（具身工具 RESTful 客户端）
│   ├── server/                     # 入口 main.go
│   ├── tests/                      # 端到端集成测试
│   └── train/                      # SFT 训练脚本（Unsloth + QLoRA）
├── tool_calling_go/                # 独立 Go SDK（Agent / Batch / BatchRace）
├── dataset/                        # 旧版 voice agent 微调数据（PPT 时代）
└── data_docs/                      # 旧版造数据指南（PPT 时代）
```

## 文档索引

| 文档 | 内容 |
|------|------|
| [`implementation/api.md`](implementation/api.md) | 后端 API 契约（语音轮次 + 双 agent 通信 + 日志流） |
| [`../async_dual_agent_system_design.md`](../async_dual_agent_system_design.md) | 异步双 agent 系统设计原理 |
| [`../api_of_embodied_tools.md`](../api_of_embodied_tools.md) | 具身工具 RESTful 网关文档（:8000） |
| [`../api_of_voice_tools.md`](../api_of_voice_tools.md) | Voice 工具 RESTful 网关文档（:8001，联调用） |
| [`../finetuning_of_arm_agent.md`](../finetuning_of_arm_agent.md) | Arm Agent 微调数据方案 |
| [`../finetuning_of_voice_agent.md`](../finetuning_of_voice_agent.md) | Voice Agent 微调数据方案 |
| [`tool_calling_go/README.md`](tool_calling_go/README.md) | tool_calling_go SDK 文档 |

## License

Apache-2.0
