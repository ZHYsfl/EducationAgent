# Voice Agent 协议规范

> 本文档固化当前代码实现中的核心协议约定，供后续开发、数据标注和调试参考。
> 代码对齐版本：2026-04-16。

---

## 1. 两回合推理架构（Two-Round Inference）

**Round 1**：后端调用一次 `StreamChat`，模型输出 `TTS 文本` + `<action>...</action>` 标签。action 被同步执行，结果以 `tool` 消息形式暂存，但**不立刻发给模型**。

**Round 2（条件触发）**：只有当 Round 1 的 action 列表里包含 `fetch_from_ppt_message_queue` 时，后端才会用**更新后的 history**（包含 Round 1 的 assistant + tool 结果）发起**第二次** `StreamChat`。这次模型只输出汇报 TTS，**不能再输出新 action**。

如果 Round 1 没有 `fetch_from_ppt_message_queue`（比如只有 `send_to_ppt_agent` 或 `update_requirements`），**不触发 Round 2**，直接 `turn_end`。

---

## 2. 历史消息顺序

一个完整 turn 的历史追加顺序必须是：

```
user (含 <status> 和 <user> 标签)
→ assistant (Round 1: TTS + 所有 <action> 标签连续拼接)
→ tool (每个 action 对应一个独立 tool 消息，按执行顺序)
→ [可选] assistant (Round 2: 汇报 TTS，仅当有 fetch 时)
→ user (下一个 turn)
```

**关键约束**：
- `tool` 是独立的 `role: "tool"` 消息，**没有 `<tool>` XML 包装**。
- Round 2 的汇报 TTS 必须是**独立的 assistant 消息**，不能和 Round 1 的 action 标签合并。
- 不需要 `tool_call_id`，纯靠顺序对齐。
- 所有 action 必须在同一个 assistant 消息内**连续输出**，action 标签之间不能插入 TTS 文本。
- 只有 `fetch_from_ppt_message_queue` 允许 `TTS → action → [可选] TTS` 的形态；其他 action（`send_to_ppt_agent` 等）执行后通常不跟汇报 TTS。

---

## 3. SSE Chunk 类型与顺序

后端流式返回的 chunk 类型：

| 类型 | 说明 |
|------|------|
| `user_transcript` | 首先发出，包含完整格式化的 `user` 消息文本，供前端直接 append 到 history。 |
| `tts` | 需要播放给用户的语音文本。 |
| `action` | 解析出的 action payload，**不会播放给用户**。 |
| `tool` | action 的同步执行结果，**不会播放给用户**。 |
| `turn_end` | 本 turn 结束标记。 |

标准流顺序：
```
user_transcript → tts → [action → tool] ... → [可选 tts] → turn_end
```

---

## 4. 打断机制：前端 vs 后端职责

### 后端职责（`vad_start`）
- 收到前端传来的 1.5s 音频，跑 ASR + interrupt-check model。
- 返回 `{interrupt: true | false}` 给前端。

### 前端职责
- 如果 `interrupt: false`：继续播放 TTS，history 不变。`vad_end` 时后端返回 `ignored: true`。
- 如果 `interrupt: true`：
  1. 立即停止 TTS 播放（`clearTTSAndPlayback`）。
  2. 中止后端 SSE 流（`sse.abort()`），触发 `ctx.Err() != nil`。
  3. **等待 action 静默跑完**：如果流已经输出过 `<action>` 开头，前端必须让该 goroutine 跑完完整 action 序列。action 是静默的，用户不会感知。
  4. `vad_end` 时，根据**TTS 是否还在播放**决定 `needs_interrupted_prefix`。
  5. 把**已经 spoken 的文本截断点**通过 `interrupted_assistant_text` 告诉后端。

---

## 5. `</interrupted>` 的判定标准

**唯一标准**：打断发生时，assistant **给用户的 TTS 语音是否还在播放**。

- **TTS 还在播放**（哪怕只差一个字没播完）：下一个 `user` 消息开头加 `</interrupted>\n`。
- **TTS 已经播完**，即使 LLM stream 还在后台输出 action 标签：不加 `</interrupted>`，用正常的 `<status>...\n<user>...</user>`。

前端通过 `ttsWasActiveRef`（记录 `performInterrupt()` 瞬间 `ttsRef.current?.isActive()`）来准确判断这一点。

---

## 6. `interrupted_assistant_text` 的作用

打断时，前端知道 TTS 实际播放到哪里（`spokenText` 的截断点），但后端不知道。如果不同步，下轮推理时：

- 前端 history 有截断的 assistant 消息。
- 后端 history 没有这条消息。

`interrupted_assistant_text` 就是用来**补这个缺口的**：
- 前端把截断后保留的 assistant 文本传给后端。
- 后端在 `StreamTurn` 开头，如果有这个字段，先 `AppendVoiceHistory(assistantMessage)`，再开始新的推理。

这适用于**任何一轮**的打断：
- **Round 1 打断**：只截断了纯 TTS，`interrupted_assistant_text` = 截断后的 spoken text。
- **Round 2 打断**：汇报 TTS 被打断，`interrupted_assistant_text` = 截断后的 Round 2 spoken text（前端的 `round2Spoken`）。

**注意**：只有在 `vad_start` 返回 `interrupt: true` 的情况下，这两个字段才有意义。正常 `vad_end` 时，它们为 `false` / `""`。

---

## 7. Action 执行与 tool 消息

- action 在 `voice_agent_service.go` 里**同步执行**。
- 每遇到一个完整的 `<action>...</action>`，提取 payload，调用 executor，结果立刻作为 `tool` chunk 发给前端。
- 前端在 `turn_end` 或 `finalizeInterruptedTurn` 时，把所有 pending 的 `tool` 消息按顺序追加到 history。
- **action 标签会作为 `type: "action"` 的 SSE chunk 发给前端，但前端不会把它 enqueue 给 TTS 引擎播放**。前端只用它来更新 UI 状态（如 `acting`）和构建 history。

---

## 8. 哪些 action 触发 Round 2

只有 `fetch_from_ppt_message_queue` 触发 Round 2。

其他 action（`update_requirements`、`require_confirm`、`send_to_ppt_agent`）执行完 tool 后直接进入 `turn_end`，没有第二次推理。

`update_requirements` 和 `require_confirm` 在第一次 `send_to_ppt_agent` 调用后**永久消失**。

---

## 9. 典型场景 JSON 示例

### 9.1 正常 Phase 2：fetch + Round 2 汇报

```json
[
  { "role": "user", "content": "<status>not empty</status>\n<user>PPT 做得怎么样了</user>" },
  { "role": "assistant", "content": "我去帮您看看进度。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: the new version of the ppt is generated successfully" },
  { "role": "assistant", "content": "新版 PPT 已经生成完毕，您需要我展示出来吗？" }
]
```

### 9.2 Round 2 汇报 TTS 被打断 → 延迟到下一个 turn 再汇报

```json
[
  { "role": "user", "content": "<status>not empty</status>\n<user>PPT 怎么样了</user>" },
  { "role": "assistant", "content": "我去帮您看看进度。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: the new version of the ppt is generated successfully" },
  { "role": "assistant", "content": "新版 PPT 已经生" },
  { "role": "user", "content": "</interrupted>\n<status>empty</status>\n<user>直接发我邮箱</user>" },
  { "role": "assistant", "content": "好的，新版 PPT 已经生成完毕，我这就帮您发送。<action>send_to_ppt_agent|data:用户要求发到邮箱</action>" },
  { "role": "tool", "content": "data is sent to the ppt agent successfully" }
]
```

### 9.3 TTS 已播完、进入 action 阶段后用户说话（不加 `</interrupted>`）

```json
[
  { "role": "user", "content": "<status>not empty</status>\n<user>PPT 怎么样了</user>" },
  { "role": "assistant", "content": "我去帮您看看进度。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: the new version of the ppt is generated successfully" },
  { "role": "assistant", "content": "新版 PPT 已经生成完毕。" },
  { "role": "user", "content": "<status>empty</status>\n<user>帮我放大字体</user>" },
  { "role": "assistant", "content": "收到。<action>send_to_ppt_agent|data:用户要求放大字体</action>" },
  { "role": "tool", "content": "data is sent to the ppt agent successfully" }
]
```

### 9.4 fetch 一次取出所有消息 + Round 2 汇报

`fetch_from_ppt_message_queue` 一次会 drain 掉队列中**所有**消息并拼接成一条 tool result。

```json
[
  { "role": "user", "content": "<status>not empty</status>\n<user>有什么消息吗</user>" },
  { "role": "assistant", "content": "我先看看队列里有什么。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: the new version of the ppt is generated successfully | questions for user: 请问封面的主色调有什么偏好吗？" },
  { "role": "assistant", "content": "新版 PPT 已经生成完毕。另外 PPT Agent 问您封面主色调有偏好吗？" }
]
```
