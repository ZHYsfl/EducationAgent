package agent

import "context"

const protocolInstructions = `

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
func (p *Pipeline) buildFullSystemPrompt(ctx context.Context, includeContextQueue bool) string {
	systemPrompt := p.config.SystemPrompt

	// Layer 1: Requirements mode override (replaces base prompt)
	p.session.reqMu.RLock()
	reqSnapshot := CloneTaskRequirements(p.session.Requirements)
	p.session.reqMu.RUnlock()
	if reqSnapshot != nil && (reqSnapshot.Status == "collecting" || reqSnapshot.Status == "ready") {
		var profile *UserProfile
		if p.clients != nil {
			if pInfo, err := p.clients.GetUserProfile(ctx, reqSnapshot.UserID); err == nil {
				profile = &pInfo
			}
		}
		systemPrompt = reqSnapshot.BuildRequirementsSystemPrompt(profile)
	}

	// Layer 2: Task list context
	taskListContext := p.buildTaskListContext()
	if taskListContext != "" {
		systemPrompt += taskListContext
	}

	// Layer 3: Pending questions context
	pendingQContext := p.buildPendingQuestionsContext()
	if pendingQContext != "" {
		systemPrompt += pendingQContext
	}

	// Layer 4: Context queue messages (RAG, search results, etc.)
	if includeContextQueue {
		contextMsgs := p.drainContextQueue()
		contextPrompt := FormatContextForLLM(contextMsgs)
		if contextPrompt != "" {
			systemPrompt += contextPrompt
		}
	}

	// Layer 5: Protocol instructions
	systemPrompt += protocolInstructions

	return systemPrompt
}
