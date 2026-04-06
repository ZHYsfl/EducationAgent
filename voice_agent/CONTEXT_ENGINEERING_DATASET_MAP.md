# Context Engineering 全景图（数据集导向，集中版）

本文只做一件事：把当前 Voice Agent 的 context 管理逻辑集中讲清楚，方便你做数据集。
不改运行逻辑，不讨论可视化。

## 1）唯一拼装函数（不是唯一触发入口）

严格来说，只有“拼装函数”是唯一的：  
所有运行时系统提示词最终都通过 `ContextManager.BuildPrompt(...)` 生成。

但触发这件事的入口不止一个，当前有 3 条路径：

1. 文本输入路径（常规）
- `Session.handleTextInput` -> `Pipeline.startProcessing`
- `startProcessing` -> `buildFullSystemPrompt(ctx, true)` -> `ContextManager.BuildPrompt(...)`

2. 上下文回灌路径（idle 时触发）
- `Pipeline.EnqueueContext` -> `processContextUpdate`
- `processContextUpdate` -> `startProcessing` -> `buildFullSystemPrompt(ctx, true)` -> `ContextManager.BuildPrompt(...)`

3. 语音交互 think 路径
- `thinkLoop` -> `runThinkStream`
- `runThinkStream` -> `buildSystemPrompt(ctx)` -> `buildFullSystemPrompt(ctx, false)` -> `ContextManager.BuildPrompt(...)`
- 注意：这里是 `includeContextQueue=false`，即本轮不会拼 Layer4（contextQueue 的实时回灌消息）。

核心代码片段（入口 + 统一拼装）：

```go
// agent/session_ws.go
func (s *Session) handleTextInput(msg WSMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	ctx := s.newPipelineContext()
	go s.pipeline.startProcessing(ctx, text)
}
```

```go
// agent/pipeline_process.go
func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.history.AddUser(userText)
	systemPrompt := p.buildFullSystemPrompt(ctx, true)
	messages := p.history.ToOpenAIWithThoughtAndPrompt("", systemPrompt)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)
	// ...
}
```

```go
// agent/pipeline_system_prompt.go + agent/context_manager.go
func (p *Pipeline) buildFullSystemPrompt(_ context.Context, includeContextQueue bool) string {
	return p.contextMgr.BuildPrompt(
		p.config.SystemPrompt,
		includeContextQueue,
		p.pendingContexts,
		p.contextQueue,
		&p.pendingMu,
	)
}

func (cm *ContextManager) BuildPrompt(baseSystemPrompt string, includeContextQueue bool, pendingContexts []ContextMessage, contextQueue chan ContextMessage, pendingMu *sync.Mutex) string {
	// Layer1 -> Layer2 -> Layer3 -> Layer4 -> Layer5（固定顺序）
}
```

代码锚点：
- `agent/session_ws.go:90`
- `agent/session_ws.go:101`
- `agent/pipeline_process.go:13`
- `agent/pipeline_process.go:18`
- `agent/pipeline_ctx.go:53`
- `agent/pipeline_ctx.go:96`
- `agent/pipeline_ctx.go:102`
- `agent/pipeline_process.go:271`
- `agent/pipeline_process.go:289`
- `agent/pipeline_process.go:290`
- `agent/pipeline_system_prompt.go:55`
- `agent/context_manager.go:240`

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
- 这条链路有两处：`startProcessing`（文本）和 `outputLoop`（语音 think）

代码片段（解析 + 执行 + 回灌）：

```go
// agent/pipeline_process.go
for token := range tokenCh {
	result := p.parser.Feed(token)
	allActions = append(allActions, result.Actions...)

	for _, action := range result.Actions {
		sessionCtx := executor.SessionContext{/* 从 session/requirements 取快照 */}
		p.executor.Execute(action, sessionCtx, p.EnqueueContext)
	}
}
```

```go
// agent/pipeline_process.go (think 路径)
func (p *Pipeline) outputLoop(ctx context.Context) {
	for {
		select {
		case token := <-p.tokenCh:
			result := p.parser.Feed(token)
			for _, action := range result.Actions {
				go p.executeAction(ctx, action)
			}
		case <-ctx.Done():
			return
		}
	}
}
```

代码锚点：
- `agent/pipeline_process.go:73`
- `agent/pipeline_process.go:104`
- `agent/pipeline_process.go:299`
- `agent/pipeline_process.go:303`
- `agent/pipeline_process.go:333`
- `agent/pipeline_process.go:358`
- `internal/protocol/parser.go:36`
- `internal/executor/executor.go:52`
- `internal/executor/executor.go:86`
- `agent/pipeline_ctx.go:53`

### C. EnqueueContext 的三类分流

- `requirements_updated`：立即走 `handleRequirementsUpdate`（不入普通队列）
- `task_list_update`：登记任务并推送前端
- 其他消息：按优先级进入 `contextQueue` 或 `highPriorityQueue`

代码片段（三类分流 + idle 回灌触发）：

```go
// agent/pipeline_ctx.go
func (p *Pipeline) EnqueueContext(msg types.ContextMessage) {
	if msg.MsgType == "requirements_updated" {
		p.handleRequirementsUpdate(msg.Content)
		return
	}
	if msg.MsgType == "task_list_update" {
		// 注册任务 + 推前端
		return
	}

	if msg.Priority == "high" {
		select {
		case p.highPriorityQueue <- msg:
		default:
			log.Printf("[ctx] high priority queue full")
		}
	} else {
		select {
		case p.contextQueue <- msg:
		default:
			log.Printf("[ctx] context queue full")
		}
	}

	if p.session.GetState() == StateIdle {
		p.sessionCtxMu.RLock()
		sCtx := p.sessionCtx
		p.sessionCtxMu.RUnlock()
		if sCtx != nil && sCtx.Err() == nil {
			if p.session.CompareAndSetState(StateIdle, StateProcessing) {
				go p.processContextUpdate(sCtx, msg)
			}
		}
	}
}

func (p *Pipeline) processContextUpdate(ctx context.Context, msg types.ContextMessage) {
	prompt := fmt.Sprintf("新任务结果（%s）: %s", msg.ActionType, msg.Content)
	p.startProcessing(ctx, prompt)
}
```

代码锚点：
- `agent/pipeline_ctx.go:55`
- `agent/pipeline_ctx.go:61`
- `agent/pipeline_ctx.go:84`
- `agent/pipeline_ctx.go:90`
- `agent/pipeline_ctx.go:95`
- `agent/pipeline_ctx.go:102`

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
- 冲突问题若重试多次失败会降级到 `pendingContexts`（后续再被 Layer4 消费）

代码锚点：
- `agent/bus.go:43`
- `agent/bus.go:74`
- `agent/bus.go:93`

## 4）读取路径（prompt 如何消费 context）

`ContextManager.BuildPrompt()` 按固定顺序拼 5 层：

1. Layer1：基础系统提示词 或 需求模式提示词
2. Layer2：任务列表与活跃任务提示
3. Layer3：待回答冲突问题
4. Layer4：上下文消息（RAG/搜索/PPT回调）
5. Layer5：协议指令（`@{}` / `#{}`）

代码片段（五层拼装主函数）：

```go
// agent/context_manager.go
func (cm *ContextManager) BuildPrompt(baseSystemPrompt string, includeContextQueue bool, pendingContexts []ContextMessage, contextQueue chan ContextMessage, pendingMu *sync.Mutex) string {
	var sb strings.Builder
	sb.WriteString(cm.buildLayer1BasePrompt(baseSystemPrompt))
	if taskCtx := cm.buildLayer2TaskList(); taskCtx != "" {
		sb.WriteString(taskCtx)
	}
	if questionsCtx := cm.buildLayer3PendingQuestions(); questionsCtx != "" {
		sb.WriteString(questionsCtx)
	}
	if includeContextQueue {
		if msgCtx := cm.buildLayer4ContextMessages(pendingContexts, contextQueue, pendingMu); msgCtx != "" {
			sb.WriteString(msgCtx)
		}
	}
	sb.WriteString(protocolInstructions)
	return sb.String()
}
```

代码片段（Layer4 会 drain 队列）：

```go
// agent/context_manager.go
func (cm *ContextManager) buildLayer4ContextMessages(pendingContexts []ContextMessage, contextQueue chan ContextMessage, pendingMu *sync.Mutex) string {
	if pendingMu == nil || contextQueue == nil {
		return ""
	}

	pendingMu.Lock()
	var msgs []ContextMessage
	if len(pendingContexts) > 0 {
		msgs = append(msgs, pendingContexts...)
	}

	for {
		select {
		case msg := <-contextQueue:
			msgs = append(msgs, msg)
		default:
			goto done
		}
	}
done:
	pendingMu.Unlock()

	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[系统补充信息 - 以下是后台检索到的相关资料，供回答参考]\n")
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("\n--- 操作: %s | 类型: %s ---\n%s\n", m.ActionType, m.MsgType, m.Content))
	}
	return sb.String()
}
```

代码锚点：
- `agent/context_manager.go:240`
- `agent/context_manager.go:244`
- `agent/context_manager.go:247`
- `agent/context_manager.go:252`
- `agent/context_manager.go:258`
- `agent/context_manager.go:264`
- `agent/context_manager.go:331`
- `agent/context_manager.go:345`
- `agent/pipeline_system_prompt.go:5`

需求模式切换条件：
- `Requirements.Status` 是 `collecting` 或 `ready` 时，Layer1 改为 `BuildRequirementsSystemPrompt`。

代码锚点：
- `agent/context_manager.go:275`
- `agent/requirements.go:152`

## 5）历史与记忆（对数据集有影响）

历史写入点：
- 文本主流程：`startProcessing` 开始写 user
- 语音 think 流程：`runThinkStream` 也会写 user（增量输入）
- 流式结束：写 assistant（可见文本）
- 被打断：写入 `AddInterruptedAssistant`

代码锚点：
- `agent/pipeline_process.go:16`
- `agent/pipeline_process.go:269`
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
4. 只有 `includeContextQueue=true` 时，Layer4 才会读取（drain）`contextQueue`。
5. `ContextManager.Export()` 也会读取（drain）`contextQueue`，所以它不是纯只读快照。
6. `pendingContexts` 会参与 prompt，但在 `BuildPrompt` 里不会被清空（清空逻辑只在测试辅助 `drainContextQueue` 中）。

代码锚点：
- `agent/context_manager.go:240`
- `agent/context_manager.go:264`
- `agent/context_manager.go:257`
- `agent/context_manager.go:331`
- `agent/context_manager.go:339`
- `agent/context_manager.go:76`
- `agent/context_manager.go:130`
- `agent/bus.go:18`

## 7）不改逻辑前提下的采集方案（最小闭环）

围绕每次 `startProcessing` 采集四段：

代码片段（建议采样点位）：

```go
// agent/pipeline_process.go（示意）
func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	// 1) state_before：先抓 session/pipeline 快照
	p.history.AddUser(userText)

	// 2) model_input：拿最终 systemPrompt + userText
	systemPrompt := p.buildFullSystemPrompt(ctx, true)

	// 3) model_output：边流式边记录 raw token / actions / visible text
	tokenCh := p.largeLLM.StreamChat(ctx, p.history.ToOpenAIWithThoughtAndPrompt("", systemPrompt))
	for token := range tokenCh {
		result := p.parser.Feed(token)
		_ = result.Actions
	}

	// 4) state_after：本轮结束后再抓一次快照
}
```

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

补充：语音 think 路径的 `model_output` 里可见文本来自 `Parser.ParseResult.VisibleText`（`outputLoop`），不是 `ProtocolFilter`。

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

