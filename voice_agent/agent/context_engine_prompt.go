package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

const contextEngineProtocolInstructions = `

## 动作协议
执行操作时使用: @{type|key:value|key:value}
内部思考使用: #{思考内容}（不显示给用户，用于复杂推理）

### 内部思考 #{} 使用场景
在以下情况下，先用 #{} 进行内部推理，再给出回复：
- 需要分析用户意图（是新需求、修改请求、还是闲聊？）
- 需要判断当前状态（需求收集到哪一步了？哪些信息还缺？）
- 需要选择动作（应该 update_requirements 还是 ppt_init？）
- 多任务场景下需要识别用户指的是哪个任务
- 冲突问题场景下需要判断用户在回答哪个问题

示例:
用户: "把那个课件改一下"
你: #{用户说"那个课件"，当前有2个任务：task_math（高等数学）和task_physics（大学物理）。用户最近在看物理课件的第3页，应该指的是物理课件} 好的，请问您想修改物理课件的哪里？

### 支持的动作
- update_requirements: @{update_requirements|字段:值} - 更新需求信息（包括初始化topic）
- ppt_init: @{ppt_init|topic:主题|desc:描述} - 开始制作PPT（需先收集完所有必填信息）
- ppt_mod: @{ppt_mod|task:任务ID|raw_text:用户原话} - 修改PPT（task参数可选，默认使用当前活跃任务；多任务时必须明确指定）
- kb_query: @{kb_query|query:查询内容}
- web_search: @{web_search|query:搜索关键词}
- resolve_conflict: @{resolve_conflict|context_id:xxx} - 回答冲突问题（多个冲突时必须指定）

## 需求收集流程
1. 用户提出制作PPT需求 → @{update_requirements|topic:主题}
2. 逐步询问用户（每次1-2个问题），收集信息后 → @{update_requirements|字段:值}
3. 所有必填信息收集完成 → @{ppt_init|topic:主题|desc:描述}

必填字段（12个）: topic, subject, audience, total_pages, knowledge_points, teaching_goals, teaching_logic, key_difficulties, duration, global_style, interaction_design, output_formats

## PPT修改流程
- 单任务场景: @{ppt_mod|raw_text:用户原话} （task参数可省略）
- 多任务场景: 根据用户提到的任务主题，明确指定task参数 @{ppt_mod|task:任务ID|raw_text:用户原话}

示例:
用户: "帮我做个高等数学的PPT"
你: 好的。@{update_requirements|topic:高等数学} 请问目标听众是谁？
用户: "大学生"
你: 明白了。@{update_requirements|audience:大学生} 需要多少页？

用户: "把物理课件的第3页改成蓝色"
你: 好的。@{ppt_mod|task:task_physics_id|raw_text:把第3页改成蓝色}
`

// buildFullSystemPrompt constructs the complete system prompt with all context layers.
// includeContextQueue: if true, drains and includes context messages from queue.
// DEPRECATED: Use p.contextMgr.BuildPrompt() directly instead.
func (p *Pipeline) buildFullSystemPrompt(_ context.Context, includeContextQueue bool) string {
	if p.contextMgr == nil {
		p.contextMgr = NewContextManager(p.session)
	}
	return p.contextMgr.BuildPrompt(p.config.SystemPrompt, includeContextQueue, p.pendingContexts, p.contextQueue, &p.pendingMu)
}

// BuildPrompt is the ONLY entry point for system prompt construction
func (cm *ContextManager) BuildPrompt(baseSystemPrompt string, includeContextQueue bool, pendingContexts []ContextMessage, contextQueue chan ContextMessage, pendingMu *sync.Mutex) string {
	var sb strings.Builder

	// Layer 1: Base or Requirements mode
	sb.WriteString(cm.buildLayer1BasePrompt(baseSystemPrompt))

	// Layer 2: Task list
	if taskCtx := cm.buildLayer2TaskList(); taskCtx != "" {
		sb.WriteString(taskCtx)
	}

	// Layer 3: Pending questions
	if questionsCtx := cm.buildLayer3PendingQuestions(); questionsCtx != "" {
		sb.WriteString(questionsCtx)
	}

	// Layer 4: Context messages (RAG/search/PPT)
	if includeContextQueue {
		if msgCtx := cm.buildLayer4ContextMessages(pendingContexts, contextQueue, pendingMu); msgCtx != "" {
			sb.WriteString(msgCtx)
		}
	}

	// Layer 5: Protocol instructions
	sb.WriteString(contextEngineProtocolInstructions)

	return sb.String()
}

// buildLayer1BasePrompt builds the base system prompt or requirements mode override
func (cm *ContextManager) buildLayer1BasePrompt(baseSystemPrompt string) string {
	cm.session.reqMu.RLock()
	reqSnapshot := CloneTaskRequirements(cm.session.Requirements)
	cm.session.reqMu.RUnlock()

	if reqSnapshot != nil && (reqSnapshot.Status == "collecting" || reqSnapshot.Status == "ready") {
		return reqSnapshot.BuildRequirementsSystemPrompt(nil)
	}

	return baseSystemPrompt
}

// buildLayer2TaskList builds the task list context (exported for testing)
func (cm *ContextManager) buildLayer2TaskList() string {
	cm.session.activeTaskMu.RLock()
	defer cm.session.activeTaskMu.RUnlock()

	if len(cm.session.OwnedTasks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 当前用户的 PPT 任务列表]\n")
	for tid, topic := range cm.session.OwnedTasks {
		marker := ""
		if tid == cm.session.ActiveTaskID {
			marker = " (当前活跃)"
		}
		sb.WriteString(fmt.Sprintf("- task_id=%s, 主题=\"%s\"%s\n", tid, topic, marker))
	}
	if len(cm.session.OwnedTasks) > 1 {
		sb.WriteString("\n用户可能用简称、缩写、别名来指代某个任务（例如用\"高数\"指\"高等数学\"）。\n")
		sb.WriteString("请根据语义判断用户说的是哪个任务。如果确实无法判断，主动追问用户，绝不要猜。\n")
		sb.WriteString("默认操作当前活跃的任务，除非用户明确提到了其他任务。\n")
	}
	return sb.String()
}

// buildLayer3PendingQuestions builds the pending questions context (exported for testing)
func (cm *ContextManager) buildLayer3PendingQuestions() string {
	cm.session.pendingQMu.RLock()
	defer cm.session.pendingQMu.RUnlock()

	if len(cm.session.PendingQuestions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 待回答的冲突问题]\n")
	sb.WriteString("以下是 PPT Agent 提出的需要用户确认的问题，请判断用户是否在回答这些问题：\n")
	for cid, pq := range cm.session.PendingQuestions {
		sb.WriteString(fmt.Sprintf("- context_id=%s, task_id=%s\n  问题: %s\n", cid, pq.TaskID, pq.QuestionText))
	}
	if len(cm.session.PendingQuestions) > 1 {
		sb.WriteString("\n有多个待确认问题，请使用动作标记指定：")
		sb.WriteString("@{resolve_conflict|context_id:xxx}\n")
	}
	return sb.String()
}

// buildLayer4ContextMessages builds the context messages from queue
func (cm *ContextManager) buildLayer4ContextMessages(pendingContexts []ContextMessage, contextQueue chan ContextMessage, pendingMu *sync.Mutex) string {
	if pendingMu == nil || contextQueue == nil {
		return ""
	}

	// Drain context queue
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

	// Format messages
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
