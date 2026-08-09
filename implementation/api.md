# 后端 API 契约 —— 异步双 Agent 具身系统（Voice Agent + Arm Agent）

> 本文是 Go 后端（Gin，默认 `:8080`）的完整 API 契约。
> 系统设计见 `../async_dual_agent_system_design.md`；具身工具网关（:8000）见 `../api_of_embodied_tools.md`；微调数据格式见 `../finetuning_of_arm_agent.md` / `../finetuning_of_voice_agent.md`。

## 通用约定

- 除 SSE 端点外，所有 REST 端点 **HTTP 恒 200**，业务码在信封里：

```json
{ "code": 200, "message": "success", "data": ... }
```

`code != 200` 表示失败，`message` 中有原因。

- 两条系统级 FIFO 队列由编排运行时（`AppState`）持有：
  - `message_from_voice_agent_queue`（voice → arm）：`send_to_arm_agent` 生产，Arm Agent 消费。
  - `message_from_arm_agent_queue`（arm → voice）：`send_to_voice_agent` 生产，Voice Agent 消费。
- 人优先原则：队列消息只能通过「`<queue_status>` 状态栏感知 + 主动消费」进入上下文，不得插队打断当前推理。

---

## Module 0：语音轮次 API（前端 VAD/播放器 ⇄ 后端）

前端职责：浏览器 VAD（含回声消除、防吞字缓存）、TTS 播放、打断时停播并中止 SSE。

### 0.1 `POST /api/v1/voice/vad_start`

说话开始的 1.5s 音频预检：ASR 转录 + 打断检测模型判断是否为真实打断。

- 请求：`{ "audio": "<base64 wav>", "format": "pcm" }`
- 响应：`data = { "interrupt": true|false }`
- 副作用：结果缓存在后端（`SetLastVADInterrupt`），供紧随其后的 `vad_end` 使用。打断检测失败时 fail-open 为 `true`。

### 0.2 `POST /api/v1/voice/vad_end`

说话结束：完整 ASR → Voice Agent 流式推理，SSE 返回。

- 请求：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `audio` | string | base64 wav（与 `text` 二选一） |
| `text` | string | 直接给文本时跳过 ASR |
| `needs_interrupted_prefix` | bool | 上一轮播报被打断时为 true，用户消息前加 `</interrupted>` |
| `interrupted_assistant_text` | string | 被打断的 assistant 已播报文本，先补入历史再推理 |

- 响应分支：
  1. 缓存的 vad_start 判定 `interrupt=false` → 普通 JSON `{code:200, data:{ignored:true}}`，不推理。
  2. 无缓存的 vad_start → `{code:400, message:"vad_start required before vad_end"}`。
  3. 否则 `Content-Type: text/event-stream`，逐行 `data: <SSEChunk JSON>\n\n`，以 `data: [DONE]` 结束。

### 0.3 `POST /api/v1/voice/text_input`

跳过 ASR 直接以文本触发一轮 Voice Agent（调试/打字备用），SSE 格式同 vad_end。请求 `{ "text": "..." }`。

### 0.4 SSE chunk 契约

```json
{ "type": "user_transcript|tts|action|tool|turn_end", "text": "...", "payload": "..." }
```

| type | 含义 |
| --- | --- |
| `user_transcript` | 完整格式化后的用户消息（含 `</interrupted>` 前缀）或独立的 `<queue_status>` 状态栏消息，前端据此先落历史；每轮先落用户消息、再落状态栏消息 |
| `tts` | 口语文本片段，前端逐段送 TTS 播放 |
| `action` | 一个完整 `<tool_call>` 的 payload（紧凑格式 `name:自由文本`，如 `send_to_arm_agent:抓取 red 物块…`），仅展示，不播放 |
| `tool` | 工具执行结果字符串（如 `发送成功` / `all_messages_from_arm_agent:…`） |
| `turn_end` | 本轮结束 |

### 0.5 上下文重组规则（打断）

- 被打断的 assistant 消息以截断文本原样保留在历史中；新用户消息以 `</interrupted>` 开头，后接用户新文本。
- 每条人类 user 消息之后紧跟一条独立的 role=user `<queue_status>empty|not empty</queue_status>` 状态栏消息（反映 `message_from_arm_agent_queue` 是否非空）；当 tool response、user input、状态栏三者同时存在时，顺序固定为 tool response → user input → 状态栏。
- 工具结果以 **tool/user 双角色**写回历史（一条 role=tool + 一条同内容 role=user）。

### 0.6 Voice Agent 两轮推理

Round 1 流式输出 TTS + `<tool_call>`；若工具调用为 `get_message_from_arm_agent`，后端执行工具、写回结果后自动发起 Round 2，模型只用口语转述消费到的消息，不得再输出新 `<tool_call>`。

---

## Module 1：跨 Agent 通信 REST 端点（联调/测试用）

> 生产路径上这些消息由两个 agent 自己的工具产生/消费；以下端点供前端面板与联调直接操作同一队列后端。

### 1.1 `POST /api/v1/send_to_arm_agent`

把一条任务/变更/取消消息追加到 `message_from_voice_agent_queue`；Arm Agent 空闲时会自动消费并启动运行时。

- 请求：`{ "from": "voice_agent", "to": "arm_agent", "content": "抓取 red 物块并放到 (1.0,2.0,3.0)。" }`
- 响应：`data = "发送成功"`；`content` 为空时 `code=400`。

### 1.2 `POST /api/v1/get_message_from_arm_agent`

排空 `message_from_arm_agent_queue`。

- 请求：空 body `{}`。
- 响应：队列非空 `data = "all_messages_from_arm_agent:消息1;消息2"`（入队顺序，`;` 拼接）；空队列 `data = "当前没有新消息"`。

### 1.3 `POST /api/v1/send_to_voice_agent`

把一条 arm 侧消息追加到 `message_from_arm_agent_queue`（模拟 Arm Agent 上报）。

- 请求：`{ "from": "arm_agent", "to": "voice_agent", "content": "已到达目标位置，任务完成。" }`
- 响应：`data = "发送成功"`。

### 1.4 `POST /api/v1/start_conversation`

重置整段会话：停止 Arm Agent 运行时、清空双队列与双方历史。

---

## Module 2：Arm Agent 运行时（内部机制，非 HTTP 契约）

- **生命周期**：`OnVoiceMessage` 只入队；运行时在空闲时启动（**空闲自动消费**：启动前排空队列，消息以一条 user 消息 `all_messages_from_voice_agent:…` 进入上下文，**不追加状态栏**——队列刚排空恒为 empty），运行中绝不因新消息重启。
- **工具调用循环**：Arm LLM 以紧凑格式 `<tool_call>\nname:args\n</tool_call>` 输出工具调用；编排层解析执行后，结果以 tool/user 双角色写回，**每条工具结果之后追加一条 role=user 的 `<queue_status>empty|not empty</queue_status>`**；模型看到 `not empty` 应调用 `get_message_from_voice_agent` 主动消费。
- **工具路由**：4 个具身工具走 `ARM_GATEWAY_BASE_URL`（默认 `http://127.0.0.1:8000`）RESTful 网关；2 个通信工具（`send_to_voice_agent` / `get_message_from_voice_agent`）直接操作 `AppState` 队列，返回字符串与 `../api_of_embodied_tools.md` §2.5/§2.6 逐字一致。
- **上下文耗尽保护**：单轮最多 32 次工具调用。

---

## Module 3：Arm Agent 日志流

### 3.1 `GET /api/v1/arm/log-stream`（SSE）

实时广播 Arm Agent 活动日志，供前端面板展示。每行 `data: <JSON 字符串>`，前缀约定：

| 前缀 | 含义 |
| --- | --- |
| `[tool] name args` | 工具调用 |
| `[tool_result] name: result` | 工具返回 |
| `[agent] text` | 最新 assistant 文本（规划/说明） |
| `[error] ...` | 运行时错误 |

带 500 条 ring buffer，晚订阅的客户端会先收到回放。

---

## 端点汇总

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/voice/vad_start` | 语音开始预检（ASR + 打断检测） |
| POST | `/api/v1/voice/vad_end` | 语音结束 → SSE 语音轮次 |
| POST | `/api/v1/voice/text_input` | 文本输入 → SSE 语音轮次 |
| POST | `/api/v1/send_to_arm_agent` | 生产 voice→arm 消息 |
| POST | `/api/v1/get_message_from_arm_agent` | 消费 arm→voice 全部消息 |
| POST | `/api/v1/send_to_voice_agent` | 生产 arm→voice 消息（联调） |
| POST | `/api/v1/start_conversation` | 重置会话 |
| GET | `/api/v1/arm/log-stream` | Arm Agent 活动日志 SSE |
