# Voice Agent 微调数据集构建文档

> 本文档基于 2026-04-15/16 会议讨论内容编写，**与当前代码实现严格对齐**。  
> 造数据前请务必通读本文档，尤其注意 **Phase 划分、消息格式、打断规则、tool 角色处理** 四部分。

---

## 1. 概述与目标

本数据集用于 **Supervised Fine-Tuning (SFT)** 我们的 Voice Agent LLM。Voice Agent 是一个语音交互助手，负责：

- **Phase 1**：收集用户的 PPT 需求（topic、style、total_pages、audience）。
- **Phase 2**：在 PPT 生成过程中，作为用户与 PPT Agent 之间的沟通桥梁（传递反馈、获取 PPT Agent 的消息）。

数据集需要覆盖 **正常流程** 和 **打断流程** 两类场景，确保模型能学会：
1. 自然对话（TTS 文本生成）。
2. 在合适的时机输出 `<action>...</action>` 动作标签。
3. 正确理解队列状态（`empty` / `not empty`）并决定下一步动作。
4. 处理用户打断时的上下文截断与恢复。

---

## 2. 全局格式规则（与代码严格对齐）

### 2.1 角色（Role）

对话历史使用以下 4 种角色：

| 角色 | 说明 |
|------|------|
| `system` | 系统提示，定义当前阶段的行为规范。由脚本统一拼接，**造数据时不需要写**。 |
| `user` | 用户输入。格式固定为 `<status>...</status>\n<user>...</user>`。 |
| `assistant` | 模型输出。**只包含说话文本 + `<action>` 标签**，不包含工具结果。 |
| `tool` | 工具执行结果。**独立的消息**，紧跟在对应的 `assistant` 消息之后。 |

**⚠️ 重要**：当前协议已完全移除 `<tool>...</tool>` XML 包装。`tool` 是一个独立的 `role`，不是 `assistant` 内容的一部分。

### 2.2 `user` 消息格式

```text
<status>{empty | not empty}</status>
<user>{用户说的话}</user>
```

- `status` 表示 **用户说完话瞬间（vad_end）** PPT Message Queue 的状态。
  - `empty`：队列为空。
  - `not empty`：队列中有 PPT Agent 发来的消息（如新版本完成、问题、冲突等）。
- `</status>` 和 `<user>` 之间必须有一个换行符 `\n`，**除此之外 `user` 消息内容里不允许有其他换行符**。
- 如果当前 turn 是**打断**触发的，需在开头加 `</interrupted>`，`</interrupted>` 和 `<status>` 之间也必须有一个换行符 `\n`：

```text
</interrupted>\n<status>{empty | not empty}</status>\n<user>{用户说的话}</user>
```

### 2.3 `assistant` 消息格式

`assistant` 消息**只能是以下两种形态之一**：

**A. 纯对话（无 action）**
```text
好的，我明白了。
```

**B. 对话 + action 标签**
```text
好的，请问风格偏好是什么？<action>update_requirements|topic:数学</action>
```

规则：
- `assistant` 的**说话文本中不允许出现换行符**（`\n`）。
- Phase 1 的基本顺序是：**TTS 文本 → action(s)**。action 在所有说话文本之后输出。
- Phase 2 中，`fetch_from_ppt_message_queue` 拿到结果后，允许在同一 turn 内继续输出一段 TTS 向用户汇报结果，顺序为：**TTS 文本 → action(s) → [可选] TTS 文本**。所有 action 必须在中间连续输出；**汇报文本之后不能再出现新的 action**。其他 action（如 `send_to_ppt_agent`）通常不跟汇报 TTS，直接进入下一个 turn。
- 一个 turn 内允许出现**多个 action**，直接拼接：
  ```text
  <action>fetch_from_ppt_message_queue</action><action>send_to_ppt_agent|data:用户觉得字体太小了</action>
  ```
- **没有 action 时，绝对不能写空 action 标签**。

### 2.4 `tool` 消息格式

`tool` 是**独立的消息**，`content` 为纯文本结果，无 XML 包装。

```json
{
  "role": "tool",
  "content": "all fields are updated"
}
```

历史顺序规则：
1. `user` 消息
2. `assistant` 消息（包含该 turn 的所有 `<action>`）
3. 按顺序排列的 `tool` 消息（每个 `<action>` 对应一个 `tool`）
4. **如果该 turn 有汇报 TTS（post-action TTS），它是一个独立的 `assistant` 消息，紧跟在所有 `tool` 之后。**

也就是说，标准的 Phase 2 汇报形态在历史中表现为：
```json
{ "role": "assistant", "content": "TTS + <action>...</action>" },
{ "role": "tool", "content": "..." },
{ "role": "assistant", "content": "汇报 TTS（可选，也可能没有）" }
```

**不需要 tool_call_id**。我们的自定义协议完全依赖顺序对齐。

### 2.5 Action 空间

Voice Agent 支持以下 action（`<action>` 标签内的格式）：

| Action | 格式 | 出现阶段 |
|--------|------|----------|
| `update_requirements` | `<action>update_requirements\|topic:...\|style:...\|total_pages:...\|audience:...</action>` | Phase 1 |
| `require_confirm` | `<action>require_confirm</action>` | Phase 1 |
| `send_to_ppt_agent` | `<action>send_to_ppt_agent\|data:...</action>` | Phase 1 & 2 |
| `fetch_from_ppt_message_queue` | `<action>fetch_from_ppt_message_queue</action>` | Phase 1 & 2 |

注意：
- `update_requirements` 和 `require_confirm` 在**第一次调用 `send_to_ppt_agent` 之后永久消失**。
- Action 参数顺序不强制，但建议按 `topic|style|total_pages|audience` 书写。

### 2.6 Tool 返回值规范（与代码对齐）

每个 action 被同步执行后，都会产生一个 `role: "tool"` 的消息。`content` 为**纯文本结果**，格式如下：

| Action | 情况 | Tool `content` 示例 |
|--------|------|---------------------|
| `update_requirements` | 成功，还有缺失字段 | `we now still missing style, total_pages, audience` |
| `update_requirements` | 成功，字段已完整 | `all fields are updated` |
| `update_requirements` | 失败 | `failed to update the requirements,please try again` |
| `require_confirm` | 成功 | `data is sent to the frontend successfully` |
| `require_confirm` | 失败 | `failed to send the data to the frontend` |
| `send_to_ppt_agent` | 成功 | `data is sent to the ppt agent successfully` |
| `send_to_ppt_agent` | 失败 | `failed to send the data to the ppt agent` |
| `fetch_from_ppt_message_queue` | 成功，队列有消息 | `the ppt message is: the new version of the ppt is generated successfully | questions for user: 请问封面的主色调有什么偏好吗？ | conflict: 用户要求插入视频，但当前模板不支持，是否换模板？ | notification: PPT 已自动保存到云端` |
| `fetch_from_ppt_message_queue` | 成功，队列为空 | `queue is empty` |
| `fetch_from_ppt_message_queue` | 失败 | `failed to fetch the data from the ppt message queue` |

**目前阶段所有数据统一按工具执行成功构造，不需要考虑失败场景。** `fetch_from_ppt_message_queue` 的非空消息内容建议覆盖三种类型：**新版本完成**、**向用户提问**、**冲突/疑问**。

---

## 3. 打断规则（核心机制）

### 3.1 打断只能发生在 TTS 阶段

这是本系统最重要的设计约束：

- **用户只能在 assistant "说话"（输出 TTS 文本）时打断。**
- **一旦 `<action>` 标签开始输出，该 action 序列必须执行到结束，不可能被打断。**

为什么？因为 action 执行是"静音"的（不播放语音），即使用户此时在说话，action 也可以在后台安全跑完。

### 3.2 打断后的历史构造

打断时进入历史的 assistant 消息，其**文本部分必须是 TTS 已经播放给用户听的 `spoken text`**，而不是 LLM 已经生成但还没播放的内容。

打断的临界点以 **TTS 是否已经播放完毕** 为准：

1. **TTS 阶段被打断，且 assistant 给用户的话还没播完**：  
   只保留**已经播放的 `spoken text`**（哪怕是一句完整的话在最后一个字之前被打断，也要截断）。未播放的部分全部丢弃。

2. **TTS 已经播放完毕（即使 action 标签还没有从 LLM 流里出来）**：  
   由于 action 是静默执行的，stream 会在后台继续跑完整个 action 序列直到结束。此时该 turn **不算被打断**，历史里应保留：**已播放的 `spoken text`（即完整播完的话）+ 完整的 `<action>` 序列**。

示例：
- Assistant TTS 已播放到：`"好的，请问风格偏好是..."`（LLM 可能已在后台生成了更多内容，但 TTS 只播到这里）
- 用户在 `"偏好是"` 之后打断，说 `"topic is math"`。
- 历史中的 assistant 消息：
  - 若 TTS 还没播完：`好的，请问风格偏好是`（仅保留已播放的 spoken text，截断）
  - 若 TTS 已经播完（哪怕 action 标签还在 stream 后面）：`好的，请问风格偏好是什么？<action>update_requirements|topic:数学</action>`（完整 spoken text + 完整 action 序列）

### 3.3 何时添加 `</interrupted>`

`</interrupted>` **只在以下情况出现**：
- 前一个 assistant **还没播完给用户的 TTS 话**（即用户听到的语音还在播放，或者 stream 尚未开始输出 action），
- 且用户在此期间发声并触发了打断。

此时下一个 `user` 消息的格式为：

```text
</interrupted>\n<status>{empty | not empty}</status>\n<user>{用户说的话}</user>
```

**如果前一个 assistant 给用户的 TTS 话已经播放完毕**（即用户已经听不到 assistant 在说话），即使后续 stream 里还在输出 action 或者用户说话了，也不算打断。此时下一个 `user` 消息**不要**加 `</interrupted>`，使用正常的 `status + user` 格式即可。

> 一句话：是否算打断，取决于 **TTS 是否还在给用户播放**，而不是 stream 里是否已经有 `<action>` 字符出现。

> **注意：action 执行与用户说话是并行的。** 系统必须等待 action 全部输出完且 tool 结果返回后，才将 `tool` 消息拼入历史。**一个完整 turn 的历史顺序为：`user` → `assistant`（含所有 TTS 与 action）→ 按顺序排列的 `tool` 消息 → [可选] 独立的 `assistant`（post-action 汇报 TTS）**。

---

## 4. Phase 1：需求收集阶段

### 4.1 阶段说明

Phase 1 从对话开始到 **第一次 `send_to_ppt_agent` 成功调用** 为止。  
该阶段 Voice Agent 的核心任务是收集并确认 4 个需求字段：
- `topic`
- `style`
- `total_pages`
- `audience`

**注意：Phase 1 期间 PPT Agent 尚未启动，因此所有 `user` 消息的 `status` 必须是 `empty`。**

### 4.2 数据类别与配比

**Phase 1 数据（仅指包含 action 的数据）占总数据量的 30%。**

内部对半分：

| 子类别 | 占比 | 说明 |
|--------|------|------|
| 有 action，未打断 | 15% | 正常收集需求、确认、发送的标准流程 |
| 有 action，打断 | 15% | 用户在 assistant 说话时打断，多样性要丰富 |

### 4.3 Phase 1 示例

#### 示例 A：未打断，逐步收集需求

```json
[
  { "role": "system", "content": "You are a helpful voice assistant..." },
  { "role": "user", "content": "<status>empty</status>\n<user>我想做一个关于数学的 PPT</user>" },
  { "role": "assistant", "content": "好的，请问风格偏好是什么？<action>update_requirements|topic:数学</action>" },
  { "role": "tool", "content": "we now still missing style, total_pages, audience" },
  { "role": "user", "content": "<status>empty</status>\n<user>风格要简洁优雅，15页，面向中学生</user>" },
  { "role": "assistant", "content": "好的，请确认以下需求是否正确。<action>update_requirements|style:简洁优雅|total_pages:15|audience:中学生</action><action>require_confirm</action>" },
  { "role": "tool", "content": "all fields are updated" },
  { "role": "tool", "content": "data is sent to the frontend successfully" },
  { "role": "user", "content": "<status>empty</status>\n<user>确认</user>" },
  { "role": "assistant", "content": "好的，开始为您生成 PPT。<action>send_to_ppt_agent|data:topic:数学, style:简洁优雅, total_pages:15, audience:中学生</action>" },
  { "role": "tool", "content": "data is sent to the ppt agent successfully" }
]
```

#### 示例 B：打断发生在 TTS 阶段（话说到一半）

```json
[
  { "role": "system", "content": "You are a helpful voice assistant..." },
  { "role": "user", "content": "<status>empty</status>\n<user>我想做一个 PPT</user>" },
  { "role": "assistant", "content": "好的，请问主题是什" },
  { "role": "user", "content": "</interrupted>\n<status>empty</status>\n<user>主题是数学</user>" },
  { "role": "assistant", "content": "请问风格偏好是什么？<action>update_requirements|topic:数学</action>" },
  { "role": "tool", "content": "we now still missing style, total_pages, audience" }
]
```

#### 示例 C：延迟调用 action（连续打断导致 action 延迟多轮）

assistant 每轮都准备说完 TTS 并调用 `update_requirements`，但用户连续在 TTS 阶段打断，导致 action 一直发不出去。直到最后一轮终于不再打断，才把之前累积应调用的 action 补齐。

```json
[
  { "role": "system", "content": "You are a helpful voice assistant..." },
  { "role": "user", "content": "<status>empty</status>\n<user>我想做一个 PPT</user>" },
  { "role": "assistant", "content": "好" },
  { "role": "user", "content": "</interrupted>\n<status>empty</status>\n<user>物理方面的</user>" },
  { "role": "assistant", "content": "请问总页数" },
  { "role": "user", "content": "</interrupted>\n<status>empty</status>\n<user>十五页</user>" },
  { "role": "assistant", "content": "请问面向的受众是谁？<action>update_requirements|topic:物理|total_pages:15</action>" },
  { "role": "tool", "content": "we now still missing style, audience" }
]
```

---

## 5. 纯对话示例（无 action）

纯对话数据占总数据量的 30%，可以出现在 Phase 1 或 Phase 2 的 system prompt 下。唯一硬性约束是 `status` 必须为空。

**纯对话里，打断和不打断各占一半。** 打断时同样只保留已播放的 `spoken text`。

#### 未打断的纯对话

```json
[
  { "role": "system", "content": "You are a helpful voice assistant..." },
  { "role": "user", "content": "<status>empty</status>\n<user>你好</user>" },
  { "role": "assistant", "content": "你好！我是你的 PPT 助手，请问需要我帮你做什么主题的演示文稿呢？" },
  { "role": "user", "content": "<status>empty</status>\n<user>我先随便看看</user>" },
  { "role": "assistant", "content": "没问题，随时告诉我你的需求即可。" }
]
```

也可以完全跳出 PPT 业务，进行生活化的闲聊，只要 `status` 为空且没有 action 即可：

```json
[
  { "role": "system", "content": "You are a helpful voice assistant..." },
  { "role": "user", "content": "<status>empty</status>\n<user>今天天气不错啊</user>" },
  { "role": "assistant", "content": "是啊，阳光明媚，适合出去走走。" },
  { "role": "user", "content": "<status>empty</status>\n<user>周末有什么推荐的户外活动吗</user>" },
  { "role": "assistant", "content": "可以去公园骑行或者爬山，既能放松又能锻炼身体。" }
]
```

#### 打断的纯对话

```json
[
  { "role": "system", "content": "You are a helpful voice assistant..." },
  { "role": "user", "content": "<status>empty</status>\n<user>最近有什么好看的电影</user>" },
  { "role": "assistant", "content": "最近上映的科幻片口碑挺" },
  { "role": "user", "content": "</interrupted>\n<status>empty</status>\n<user>我不太喜欢科幻片</user>" },
  { "role": "assistant", "content": "那悬疑片怎么样？最近有一部评分很高的。" }
]
```

---

## 6. Phase 2：反馈与通信阶段

### 6.1 阶段说明

Phase 2 从 **第一次 `send_to_ppt_agent` 调用之后** 开始。  
此时需求已确认并发送给 PPT Agent，Voice Agent 的职责变为：

1. **与用户闲聊/解释进度**（纯对话）。
2. **从 PPT Message Queue 拉取消息**（`fetch_from_ppt_message_queue`）。
3. **将用户反馈转发给 PPT Agent**（`send_to_ppt_agent|data:...`）。

### 6.2 关键状态：队列空 vs 不空

Phase 2 的 `status` 可以是 `empty` 或 `not empty`，这取决于用户说完话时 PPT Message Queue 的状态：

- **`empty`**：队列里没有新消息。通常不需要 `fetch`。
- **`not empty`**：队列里有消息。通常下一步需要 `fetch_from_ppt_message_queue`。

**PPT Message Queue 中可能出现的消息类型**：

| 消息类型 | 示例内容 |
|----------|----------|
| 新版本完成 | `the new version of the ppt is generated successfully` |
| 向用户提问 | `questions for user: 请问封面的主色调有什么偏好吗？` |
| 冲突/疑问 | `conflict: 用户要求的 5 页与之前确认的 15 页不一致，请确认` |

### 6.3 数据类别与配比

**Phase 2 数据占总数据量的 40%。**

内部需要考虑：

- **是否打断**：未打断 / 打断（仅发生在 TTS 阶段）。
- **队列状态**：`empty` / `not empty`。
- **动作组合**：纯对话 / 只 fetch / 只 send / **fetch+send 混合（重点场景）**。
- **PPT 消息类型**：新版本完成 / 提问 / 冲突。

### 6.4 打断与汇报的延迟

Phase 2 中，`fetch_from_ppt_message_queue` 执行后可以用一段 TTS 向用户汇报结果（如"新版 PPT 已经生成完毕"）。**如果这段汇报文本在 TTS 阶段被打断**，由于 action 已经执行完毕，汇报内容**可以延迟到下一个 turn 再说**。

### 6.5 Phase 2 示例

#### 示例 D：队列不空，拉取消息后反馈给用户

```json
[
  { "role": "system", "content": "You are a helpful voice assistant..." },
  { "role": "user", "content": "<status>not empty</status>\n<user>PPT 做得怎么样了</user>" },
  { "role": "assistant", "content": "我去帮您看看进度。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: the new version of the ppt is generated successfully" },
  { "role": "assistant", "content": "新版 PPT 已经生成完毕，您需要我展示出来吗？" }
]
```

#### 示例 E：用户有反馈，转发给 PPT Agent

```json
[
  { "role": "user", "content": "<status>empty</status>\n<user>我觉得字体太小了，颜色也不够鲜艳</user>" },
  { "role": "assistant", "content": "好的，我把您的反馈转达给 PPT Agent。<action>send_to_ppt_agent|data:用户觉得字体太小，颜色不够鲜艳</action>" },
  { "role": "tool", "content": "data is sent to the ppt agent successfully" },
  { "role": "assistant", "content": "已经发送，正在处理中。" }
]
```

#### 示例 F：混合动作（fetch + send）

```json
[
  { "role": "user", "content": "<status>not empty</status>\n<user>有什么消息吗</user>" },
  { "role": "assistant", "content": "我先看看队列里有什么。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: conflict: 用户要求 10 页，但之前确认的是 15 页，请确认最终页数" },
  { "role": "assistant", "content": "PPT Agent 有一个疑问：您之前确认的是 15 页，但刚才提到 10 页，请问最终按多少页来？" },
  { "role": "user", "content": "<status>empty</status>\n<user>就按 10 页吧</user>" },
  { "role": "assistant", "content": "收到。<action>send_to_ppt_agent|data:用户确认最终页数为 10 页</action>" },
  { "role": "tool", "content": "data is sent to the ppt agent successfully" }
]
```

#### 示例 G：Phase 2 中的打断（TTS 阶段被打断）

```json
[
  { "role": "user", "content": "<status>empty</status>\n<user>PPT 怎么样了</user>" },
  { "role": "assistant", "content": "目前还没消" },
  { "role": "user", "content": "</interrupted>\n<status>not empty</status>\n<user>不用解释了，直接告诉我结果</user>" },
  { "role": "assistant", "content": "好的，我这就去拉取队列消息。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: the new version of the ppt is generated successfully" },
  { "role": "assistant", "content": "已经生成完毕！" }
]
```

#### 示例 H：已进入 action 阶段后用户说话（不加 `<interrupted>`）

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

#### 示例 I：Round 2 汇报 TTS 被打断 → 延迟到下一个 turn 再汇报

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

#### 示例 K：fetch + send 同 turn，触发 Round 2 汇报

用户要求先看消息再一并反馈。assistant 先 fetch，拿到冲突消息后，在同一 turn 内用第二个 action 把情况 send 给 PPT Agent，然后 Round 2 生成汇报 TTS。

```json
[
  { "role": "user", "content": "<status>not empty</status>\n<user>有什么消息吗，帮我一起处理了</user>" },
  { "role": "assistant", "content": "我先看看队列里有什么。<action>fetch_from_ppt_message_queue</action>" },
  { "role": "tool", "content": "the ppt message is: conflict: 用户要求 10 页，但之前确认的是 15 页，请确认最终页数" },
  { "role": "assistant", "content": "PPT Agent 说页数有冲突，我先把问题转达给他。<action>send_to_ppt_agent|data:用户之前确认15页，目前需要确认最终页数</action>" },
  { "role": "tool", "content": "data is sent to the ppt agent successfully" },
  { "role": "assistant", "content": "已经发送给 PPT Agent 了，等他处理完会再通知您。" }
]
```

---

## 7. 数据量与任务分配（参考）

会议上确定的目标总量为 **6,000 条**。三大块配比为：

| 数据块 | 占比 | 数量 | 备注 |
|--------|------|------|------|
| Phase 1（有 action） | 30% | 1,800 条 | 需求收集阶段，包含 action |
| Phase 2 | 40% | 2,400 条 | 反馈与通信阶段 |
| 纯对话（无 action） | 30% | 1,800 条 | 仅聊天，无 action，`status` 必须 `empty` |

### Phase 1 细分（1,800 条）

| 子类别 | 占比 | 数量 | 负责人 | 说明 |
|--------|------|------|--------|------|
| 有 action，未打断 | 15% | 900 条 | 中天仪 | 标准需求收集、确认、发送流程 |
| 有 action，打断 | 15% | 900 条 | 其余 3 人（每人 300 条） | TTS 阶段被打断，多样性要丰富 |

### Phase 2 细分（2,400 条）

Phase 2 需要覆盖多个维度的组合：

| 维度 | 组合说明 |
|------|----------|
| 打断 | 未打断 / 打断（仅发生在 TTS 阶段） |
| 队列状态 | `empty` / `not empty` |
| 动作组合 | 纯对话 / 只 fetch / 只 send / fetch+send 混合 |
| PPT 消息类型 | 新版本完成 / 向用户提问 / 冲突 |

### 纯对话细分（1,800 条）

- 可以混合使用 Phase 1 和 Phase 2 的 system prompt（由脚本统一处理）。
- 所有样本的 `status` **必须是 `empty`**。
- **打断和不打断各占一半**。
- 分配参考：4 人 × 450 条（每人 225 条打断 + 225 条未打断）。

---

## 8. 造数据注意事项（Do's and Don'ts）

### ✅ 必须遵守

1. **`<tool>` 标签已死，不要再出现。** `tool` 必须是独立的 `role: "tool"` 消息。
2. **Assistant 文本中不要换行。** 所有 TTS 文本应在一行内。
3. **User 消息格式固定：** `<status>...</status>\n<user>...</user>`，status 和 user 之间必须换行。
4. **单个 turn 内的输出顺序：**
   - Phase 1：`TTS 文本 → action(s)`。action 在所有说话文本之后输出。
   - Phase 2：`fetch_from_ppt_message_queue` 允许 `TTS 文本 → action → [可选] TTS 文本` 的汇报形态；其他 action（如 `send_to_ppt_agent`）通常不跟汇报 TTS。**汇报文本之后不能再有新的 action**。
5. **打断只发生在 TTS 阶段。** 不要构造 "action 执行到一半被打断" 的样本。
6. **纯对话样本的 `status` 必须是 `empty`。`** 否则模型会尝试调用工具。
7. **一个 turn 内允许连续多个 action。** 每个 action 对应一个独立的 `tool` 消息，按顺序排列在第一个 `assistant` 之后。
8. **Phase 2 的 post-action 汇报 TTS 必须是独立的 `assistant` 消息，紧跟在所有 `tool` 之后。** 不能把汇报文本和 action 标签塞在同一个 `assistant` 消息里。
8. **`update_requirements` 和 `require_confirm` 在第一次 `send_to_ppt_agent` 之后永久消失。** Phase 2 不应再出现这两个 action。
9. **Status 反映的是用户说完话瞬间（vad_end）的队列状态**，不是任意时刻的状态。
10. **action 之前的 TTS 文本不要包含对执行结果的断言**（如"已经记录了"、"已更新"），应只提问题或做自然引导。Phase 2 中，`fetch_from_ppt_message_queue` 执行后可以用一段 TTS 向用户汇报结果（如"新版 PPT 已经生成完毕"）；`send_to_ppt_agent` 等其他 action 通常不跟汇报 TTS。
11. **打断场景的多样性（entropy）要尽可能大。** 不要只造一两种打断模板，要覆盖各种打断时机、打断话术和上下文。

### ❌ 禁止出现

- 在 `assistant` 内容里写 `<tool>...</tool>`。
- 空 action 标签（如 `<action></action>`）。
- action 标签内出现换行符。
- 汇报 TTS 文本之后再次出现 action（如 `TTS → action → TTS（汇报）→ action`）。
- Phase 2 中出现 `update_requirements` 或 `require_confirm`。
- 打断发生在 `<action>` 已经输出之后。

---

## 9. 快速自查清单

提交数据前，逐条检查每个样本：

- [ ] 所有 `user` 消息都包含 `<status>` 和 `<user>` 标签，且两者之间有换行符。
- [ ] 所有 `assistant` 消息都没有换行符。
- [ ] 如果样本包含 action，则第一个 `assistant` 之后有对应数量的 `tool` 消息。
- [ ] Phase 2 的汇报 TTS（如果有）是独立的 `assistant` 消息，位于所有 `tool` 之后，不和 action 标签混在一起。
- [ ] 没有任何地方出现 `<tool>` 或 `</tool>` 标签。
- [ ] 打断样本的 `user` 消息以 `</interrupted>\n` 开头。
- [ ] 纯对话样本的 `status` 为 `empty`。
- [ ] Phase 2 样本不包含 `update_requirements` 或 `require_confirm`。
- [ ] 每个样本整体逻辑通顺，上下文连贯。

---

## 附录：动作标签速查表

```text
<action>update_requirements|topic:数学|style:简洁|total_pages:15|audience:中学生</action>
<action>require_confirm</action>
<action>send_to_ppt_agent|data:用户确认需求为 topic:数学, style:简洁, total_pages:15, audience:中学生</action>
<action>fetch_from_ppt_message_queue</action>
```

---

*文档版本：v1.0*  
*对齐代码版本：2026-04-16（独立 tool role 协议已落地）*
