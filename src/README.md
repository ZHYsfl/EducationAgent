# EducationAgent — 可打断语音课件助手

一句话架构：**浏览器**（VAD / 2s ring / 播放器 / said 账本的真身）↔ **Go 后端**（episode
打断心脏 + voice/ppt 双 agent 闭环 + ASR/TTS/LLM 引擎代理）↔ **模型服务**（ASR =
vLLM 直挂 Qwen3-ASR @ :8000；TTS = IndexTTS-2.5 的薄 HTTP wrapper @ :8001；LLM =
DeepSeek 远程流式）。打断语义（generation 验色 / llmState 三相 / 工具段免疫 / 回溯式
tryComplete）全部定稿在 `../src/voice_agent.excalidraw`，本文只讲怎么跑、怎么测、坑在哪。

## 目录导览（src/ 下）

| 路径 | 一句话 |
| --- | --- |
| `cmd/server` | 生产装配：广播 sink + 真命令 runner + ppt agent goroutine |
| `internal/config` | 环境变量加载 + 极简 dotenv 读取（`.env` 只补缺不覆盖） |
| `internal/protocol` | 六个 API 面的信封与类型（**协议唯一权威**：改协议先改 `API.md` 再改这里） |
| `internal/llm` | DeepSeek 流式客户端：SSE → `Event` 通道 + 工具调用累积器 |
| `internal/voiceengine` | 并发核心：`Engine`（generation/llmState/队列/工具记录）+ `Producer`（单 goroutine 跑一轮，句级增量下发，工具段免疫打断） |
| `internal/episode` | 打断心脏：`Manager.OnVadStart/End`（A+B+C 三刀）、`tryComplete`（回溯点+锁内点火）、`composeLocked`（`</interrupted>`/queue_status/工具史）、`OnUserText` 文字注入 |
| `internal/agents` | 双 agent：voice（phase1 收需求→phase2 桥，相变=工厂重建工具集）+ ppt（jail 文件工具、命令白名单、SlideWindow Loop）；系统提示词真源在 `agents/prompts/*.md`（go:embed 取中文版） |
| `internal/engineserver` | 传输层：2.1 WS（infer 请求/响应 + **订阅推送**）、1.1/1.2 vad HTTP 壳、句子消费器（sink 可换：dial=测试 / broadcast=生产）、`App` 装配 |
| `internal/player` | Go 侧播放账本参考实现（浏览器真身在前端，本包给同进程模式与测试用） |
| `internal/asr` | Qwen3-ASR 封装：pcm→wav→multipart→抠 `<asr_text>` |
| `agent_runtime/`（独立 module） | LLM agent 底座：工具执行（错误分类 tool 消息）、非流式 `Loop`、记忆策略 |
| `frontend/` | Vite+原生 TS 浏览器真身：mic/vad/ring/player/ws/api 六模块 |
| `tests/` | E2E（`E2E=1` 才跑）+ `tests/testdata/asr_test_16k.wav` |
| `workspace/ppt/` | ppt agent 的 Slidev 项目（jail 根），`slides.md`/`ppt.pdf` 生成于此 |

## 运行

### 环境准备（一次性）

```bash
# 0. WSL 内存（见下方坑点清单），建议 ≥16Gi

# 1. 仓库根 venv：ASR 服务（qwen-asr 0.0.6 + vllm 0.14.0 + torch 2.9.1+cu128）
cd /home/zane/sound/EducationAgent
uv venv .venv
.venv/bin/pip install qwen-asr vllm torch --index-url https://download.pytorch.org/whl/cu128
# torch 必须 cu128（见坑点）；qwen-asr 装完自带 qwen-asr-serve 命令

# 2. index-tts venv：TTS wrapper 的依赖
cd index-tts
uv sync            # ⚠️ 不要加 --all-extras：会拉 flash-attn 源码编译，巨坑
# 参考音色换法：把新 wav 放到 index-tts/checkpoints/ 并改 TTS_SPK_AUDIO 环境变量
# （默认 checkpoints/voice_ref.wav）

# 3. DeepSeek key 写进 src/.env（模板见 src/.env.example，ASR_MODEL 必须带完整路径）

# 4. Go 工具链：本机 go1.23.4，构建全部走 GOTOOLCHAIN=go1.25.0 自动切换（已缓存）
export PATH=/home/zane/sound/EducationAgent/go/bin:$PATH
```

### 一键起全栈

```bash
./deploy/start_all.sh     # ASR→TTS→Go→前端，逐路健康检查，幂等可重入
./deploy/stop_all.sh      # 按端口清场（8000/8001/8080/5173 全杀，不区分来源）
```

就绪后浏览器开 **http://127.0.0.1:5173**。

### 手动验收（5 步，真机）

1. `deploy/start_all.sh` 四路健康全过后，浏览器开 `http://127.0.0.1:5173`；
2. 点"开始对话"并授权麦克风，状态变"已连接（订阅中）"；
3. 说"我想做一个关于西湖的课件"→字幕区逐句出"正在播"、播完挪进"已播账本"；
4. 播放中途直接说话打断 → 播放应声而停，日志出现新的 `vad_start` 与 said 快照，回答恢复后字幕继续；
5. 多轮补齐风格/页数/受众并确认 → voice agent 相变，ppt agent 开始生成，日志可见导出与通知。

## 测试

```bash
export PATH=/home/zane/sound/EducationAgent/go/bin:$PATH
cd src

GOTOOLCHAIN=go1.25.0 go test ./...                      # 单测（全 fake，秒级）
GOTOOLCHAIN=go1.25.0 go test -race ./...                # 竞态门禁
E2E=1 GOTOOLCHAIN=go1.25.0 go test ./tests/ -v          # 真 DeepSeek/TTS/ASR（~1 分钟）
E2E=1 E2E_FULL=1 GOTOOLCHAIN=go1.25.0 go test ./tests/ -run PPTAgent -v
                                                        # 含真 slidev 导出 pdf（分钟级）
cd agent_runtime && GOTOOLCHAIN=go1.25.0 go test ./...  # 独立 module 别忘了

cd ../frontend && npm run build                         # tsc + vite build
```

## 运维坑点清单（血泪换的）

- **WSL 内存**：`.wslconfig` 给 `[wsl2] memory=23GB swap=16GB`。ASR+TTS+DeepSeek 流式同机时
  15Gi 会 OOM 杀进程，症状千奇百怪（wedge、curl 000、test 随机挂）。
- **torch 必须 cu128**：`torch 2.9.1+cu130` 会缺 `libcudart.so.12`，
  症状是 import/启动期就炸或卡死。装：`--index-url https://download.pytorch.org/whl/cu128`。
- **ASR 必须 `--max-model-len 8192`**：不传则 KV cache 按默认长度算，16G 卡直接 OOM。
- **TTS wrapper 会积压**：客户端中途断开（测试进程退出、打断）后，在飞的合成要在
  锁内排完队才释放（`gen.close()` 已修真正的泄漏，`_model_lock` 有 60s 获取超时），
   abandoned 句子多时会形成几十秒的积压，表现为后续请求超时。兜底 = 重启 TTS
  （杀 :8001 监听进程后重跑 `deploy/start_all.sh`，幂等只起缺的）；Go 侧已有三重
  防御（20s header 超时 / 15s 帧超时 / 30s body 看门狗），积压时单句失败而非全链路挂。
  **跑 E2E 前建议重启一次 TTS**；E2E 用例内部已带重试（黄金窗/播放相位双相位）。
- **显存预算（RTX 5080 Laptop 16GB）**：ASR `--gpu-memory-utilization 0.25`（≈4GB）+
  IndexTTS（≈8-9GB，bf16）+ chromium 导出（≈1-2GB），余量紧；E2E_FULL 与真机对话别并行跑。
- **slidev 导出**：chromium 缺系统共享库，`RealCommandRunner` 已自动预挂
  `~/miniconda3/envs/chromedeps/lib`；playwright-chromium 1.63 已在 workspace/ppt
  装好（浏览器用 `~/.cache/ms-playwright/chromium-1243`）。**警惕模型手写的
  `pw-shim*.mjs`**——slidev 会优先加载它，见到就删。
- **ASR 的 model 名是完整路径**（vLLM 按 /v1/models 的 id 校验，`whisper` 之类的名字 404）。
- **E2E 的 TTS 时序敏感**：`TestBargeInTTSPhase` 的黄金窗口要求"推理中且已有落账"，
  服务冷启动（首句合成 ~20s）或被打断楔死后会超时——先探活再跑。

## API 契约

六个 API 面的请求/响应/帧序以 **`API.md`** 为唯一权威（2.1 含订阅模式、1.1 含
`said_words` 覆盖语义）。协议变更流程：先改 `API.md`，再改 `internal/protocol` 与
对端实现，最后补测试。
