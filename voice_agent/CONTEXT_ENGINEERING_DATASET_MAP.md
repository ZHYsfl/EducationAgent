# Context Engineering 全景图（数据集导向，集中版）

本文只做一件事：把当前 Voice Agent 的 context 管理逻辑集中讲清楚，方便你做数据集。
不改运行逻辑，不讨论可视化。

## 1）唯一主入口

每轮推理的上下文构造只走一条主线：

- `Pipeline.startProcessing()`
- -> `buildFullSystemPrompt(ctx, true)`
- -> `ContextManager.BuildPrompt(...)`

代码锚点：
- `agent/pipeline_process.go:13`
- `agent/pipeline_process.go:18`
- `agent/pipeline_system_prompt.go:55`
- `agent/context_manager.go:372`

## 2）上下文状态到底存在哪

真正的状态源是 `Session` 和 `Pipeline`，`ContextManager` 主要负责“读取并拼 prompt”。

核心状态：
- 需求：`Session.Requirements`
- 任务：`Session.OwnedTasks`、`Session.ActiveTaskID`、`Session.ViewingPageID`
- 冲突问题：`Session.PendingQuestions`
- 对话历史：`Pipeline.history`
- 上下文队列：`Pipeline.contextQueue`、`Pipeline.highPriorityQueue`、`Pipeline.pendingContexts`

代码锚点：
- `agent/session_types.go:43`
- `agent/session_types.go:64`
- `agent/session_types.go:69`
- `agent/session_types.go:73`
- `agent/pipeline.go:64`
- `agent/pipeline.go:65`
- `agent/pipeline.go:67`
- `agent/pipeline.go:74`

## 3）写入路径（context 如何被更新）

### A. 来自用户 WS 输入

- `text_input` -> 进入 `startProcessing`
- `page_navigate` -> 更新当前任务/页面
- `add_reference_files` -> 更新需求中的参考文件

代码锚点：
- `agent/session_ws.go:10`
- `agent/session_ws.go:16`
- `agent/session_ws.go:18`
- `agent/session_ws.go:20`
- `agent/session_ws.go:90`
- `agent/session_ws.go:217`

### B. 来自 LLM 动作执行

- 流式 token 被 `Parser` 解析出 `@{...}` 动作
- `executor.Execute(...)` 执行动作
- 通过回调把 `ContextMessage` 回灌给 `Pipeline.EnqueueContext`

代码锚点：
- `agent/pipeline_process.go:73`
- `agent/pipeline_process.go:104`
- `internal/protocol/parser.go:36`
- `internal/executor/executor.go:52`
- `internal/executor/executor.go:86`
- `agent/pipeline_ctx.go:53`

### C. EnqueueContext 的三类分流

- `requirements_updated`：立即走 `handleRequirementsUpdate`（不入普通队列）
- `task_list_update`：登记任务并推送前端
- 其他消息：按优先级进入 `contextQueue` 或 `highPriorityQueue`

代码锚点：
- `agent/pipeline_ctx.go:55`
- `agent/pipeline_ctx.go:61`
- `agent/pipeline_ctx.go:84`

### D. 需求更新落地逻辑

- 解析 JSON 字段
- 写入 `TaskRequirements`
- 计算 `CollectedFields` / `MissingFields`
- 状态置为 `collecting` 或 `ready`
- 发 `requirements_progress`，ready 时再发 `requirements_summary`

代码锚点：
- `agent/pipeline_post.go:14`
- `agent/pipeline_post.go:68`
- `agent/pipeline_post.go:69`
- `agent/pipeline_post.go:80`
- `agent/pipeline_post.go:87`

### E. 高优先级侧通道

- `highPriorityListener` 处理冲突问题与系统通知
- 冲突问题会写入 `PendingQuestions`

代码锚点：
- `agent/bus.go:43`
- `agent/bus.go:74`

## 4）读取路径（prompt 如何消费 context）

`ContextManager.BuildPrompt()` 按固定顺序拼 5 层：

1. Layer1：基础系统提示词 或 需求模式提示词
2. Layer2：任务列表与活跃任务提示
3. Layer3：待回答冲突问题
4. Layer4：上下文消息（RAG/搜索/PPT回调）
5. Layer5：协议指令（`@{}` / `#{}`）

代码锚点：
- `agent/context_manager.go:372`
- `agent/context_manager.go:402`
- `agent/context_manager.go:415`
- `agent/context_manager.go:441`
- `agent/context_manager.go:463`
- `agent/pipeline_system_prompt.go:5`

需求模式切换条件：
- `Requirements.Status` 是 `collecting` 或 `ready` 时，Layer1 改为 `BuildRequirementsSystemPrompt`。

代码锚点：
- `agent/context_manager.go:402`
- `agent/requirements.go:152`

## 5）历史与记忆（对数据集有影响）

历史写入点：
- 轮次开始：写 user
- 流式结束：写 assistant（可见文本）
- 被打断：写入 `AddInterruptedAssistant`

代码锚点：
- `agent/pipeline_process.go:16`
- `agent/pipeline_process.go:177`
- `internal/history/history.go:26`
- `internal/history/history.go:32`
- `internal/history/history.go:38`

记忆上送：
- 历史过长时 `maybeCompressHistory()` 异步推送前段
- 会话关闭时 `pushRemainingContext()` 推送剩余

代码锚点：
- `agent/pipeline_post.go:200`
- `agent/pipeline_post.go:234`
- `agent/session.go:52`
- `agent/session.go:57`

## 6）做数据集必须知道的 6 个事实

1. Prompt 构造顺序是固定的（5层），这是监督样本可重复的基础。
2. 动作执行是异步的，某些 context 更新会滞后一轮生效。
3. `requirements_updated` 是立即处理，不走普通排队。
4. Layer4 在构造 prompt 时会读取（drain）`contextQueue`。
5. `ContextManager.Export()` 也会读取（drain）`contextQueue`，所以它不是纯只读快照。
6. `pendingContexts` 会参与 prompt，但在 `BuildPrompt` 里不会被清空。

代码锚点：
- `agent/context_manager.go:463`
- `agent/context_manager.go:86`
- `agent/context_manager.go:141`
- `agent/context_manager.go:472`

## 7）不改逻辑前提下的采集方案（最小闭环）

围绕每次 `startProcessing` 采集四段：

### state_before
- requirements 全量
- 任务集合 + 活跃任务 + viewing_page
- pending_questions
- pendingContexts 长度
- contextQueue 可观测信息（若可安全读取）
- history 快照

### model_input
- 本轮最终 `systemPrompt`
- user_text

### model_output
- 原始 token 流
- 解析出的 actions（`result.Actions`）
- 可见文本（`ProtocolFilter` 后）

### state_after
- requirements 新状态与字段
- task/pending_question 的变化
- 本轮产出的 ContextMessage

监督样本可以整理成：
- `state_before + prompt + user_text -> actions + visible_reply -> state_after`

## 8）建议阅读顺序（集中理解）

1. `agent/pipeline_process.go`
2. `agent/pipeline_system_prompt.go`
3. `agent/context_manager.go`
4. `agent/pipeline_ctx.go`
5. `agent/pipeline_post.go`
6. `internal/executor/executor.go`
7. `internal/executor/ppt.go` + `internal/executor/requirements.go`

