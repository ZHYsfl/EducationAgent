核心思想就是，先1s 1s流式asr,把结果结合history，预热kv-cache（thinkLoop纯warmup，所有token丢弃），一直到vad_end且2pass的结果出来的时候，ctx.cancel()掉所有的正在推理的goroutine,然后正式推理，推理的每个周期其实是think->output->[]action或者think->[]action->output,think是#{xxx},output是xxxxx正常输出，[]action是@{xxx|xxx}@{xxxx|xxxx}这样，然后output或[]action可以为空，但是如果两者同时为空，那么#{}后一定不能再有东西了，否则两个#{}#{}挨在一起是有问题的。如果被打  
断（用户又开始vad_start),那么如果这时候是@{xxxx|xxx}是最新输出（通过检测是否有@但是后面没有}判断），必须等待}输出了再cancel()掉然后上下文加</interrupted>,因为是流式输出，每次等1s检查下就行，如果下次检查虽然上次动作闭合了但是又有新的@没闭合，继续等，最多等2s(检查两次），如果第二次发现还没闭合成功，硬性cancel,然后把@包括其后面没闭合的从上下文里删去，然后用户vad_end后<user>xxxxx</user>以此类推；如果不是这类情况都可以直接打断加</interrupted>.然后流式的时候一旦每次解析出一个@{xxx|xxx}立马执行，所有的动作都是异步的，等待后续其他模块post给结果就好，给的结果统一放context queue，工具结果以<tool>xxxx</tool>形式通过flushToolResults()在每次LLM调用前写入对话历史（role: tool），不注入system prompt。所以用go的表达方式，就是若type Tool = <tool>xxx</tool>,type User = <user>xxx</user>,type Action = @{xx|xxx},type Think=#{xxxxx},type Output = xxxx,type Model = Think+Output+[]Action / Think+[]Action+Output / Think+[]Action / Think+Output,type AI = Optional[ []Model ]+Optional[ Think ]+Optional[ </interrupted> ]的情况下，那么[]Tool随时可能穿插在User整体和AI整体之间的，通过这种方式可以管理上下文了，这下清楚了：ALL CONTEXT = User , AI , []Tool 的三人转。每次用户<user>xxx</user>的vad_start记录时间戳和PPT快照，一遍后续用（比如ppt_mod需要用户说话时候的时间戳和快照状态）。

我们的工具调用协议在voice agent上体现为以下工具：
1.@{kb_query|query:查询内容}
调用POST /api/v1/kb/query（异步）
解析到后我们自动加一些session_id和task_id等需要的字段，这些不用AI在工具调用里去生成。到时候会回调ppt_message作为<tool>xxx</tool>填充到context queue里。（成功就简短检索到的summary信息，失败就返回查询失败）
2.@{web_search|query:搜索关键词}
调用POST /api/v1/search/query（异步）,到时候会回调ppt_message作为<tool>xxx</tool>填充到context queue里。（成功就简短检索到的summary信息，失败就返回查询失败）
3.@{update_requirements|topic:主题|desc:描述|...}
更新requirements信息，用户这次说了几个参数就有几个，所有字段一共情况是：
topic，description，total_pages，audience，global_style，knowledge_points，teaching_goals，teaching_logic，key_difficulties，duration，interaction_design，output_formats。其中reference_files是可选的，而且是前端用户鼠标去选择，不是语音询问用户回答然后工具调用搞的。异步，到时候会回调ppt_message作为<tool>xxx</tool>填充到context queue里。（成功就返回成功更新xx简短信息，失败就返回更新xx失败）
4.@{require_confirm}
当topic，description，total_pages，audience，global_style，knowledge_points，teaching_goals，teaching_logic，key_difficulties，duration，interaction_design，output_formats都填好了，这个时候AI调用这个工具，请求人类肯定，然后立马就@{ppt_init}开始制作PPT。人类会在前端看到这些字段，不用require_confirm工具调用的时候输出topic,description,total_pages,audience,global_style,knowledge_points,teaching_goals,teaching_logic,key_difficulties,duration,interaction_design,output_formats这些字段。异步，到时候会回调ppt_message作为<tool>xxx</tool>填充到context queue里。（成功发送反馈就立马返回简短成功信息，失败就返回发送反馈失败）
5.@{ppt_init}
调用POST /api/v1/ppt/init（异步）,参数是上面update_requirements的参数，其中reference_files是可选的，而且是前端用户鼠标去选择，不是语音询问用户回答然后工具调用搞的,其他字段必须齐全才能成功。会立马补充ppt_message作为<tool>xxx</tool>填充到context queue里。（成功发送反馈就立马返回简短成功信息，失败就返回缺少啥字段）
6.@{ppt_mod|raw_text:用户原话|user_distance:int}
调用POST /api/v1/ppt/feedback（异步）,其中user_distance是raw_text对应的<user>xxx</user>距离@{}调用的时候，是@{}前倒数第几个<user>xxx</user>,如果用户不打断一般为1，raw_text是用户原话。得到后确定是哪个<user>xxx</user>来确定base_timestamp和base_ppt_snapshot，viewing_page_id等信息。到时候会回调ppt_message作为<tool>xxx</tool>填充到context queue里。（成功发送反馈就立马返回简短成功信息，失败就返回发送反馈失败）
7.@{get_memory|query:查询内容}
调用POST /api/v1/memory/recall（异步）,参数是query，到时候会回调ppt_message作为<tool>xxx</tool>填充到context queue里。（成功就返回简短检索到的summary信息，失败就返回查询失败）


tool_message:

工具结果通过 flushToolResults() 在每次 startProcessing 前写入对话历史，role: tool，渲染为 openai.UserMessage("<tool>type:content</tool>")。不注入 system prompt。

system_message:

Layer1:

BuildCollectionPrompt：
你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：%s\n\n已收集的信息：\n- topic: %s\n- description: %s\n- audience: %s\n- total_pages: %d\n- duration: %s\n- global_style: %s\n- knowledge_points: %s\n- teaching_goals: %s\n- teaching_logic: %s\n- key_difficulties: %s\n- interaction_design: %s\n- output_formats: %s\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。

Layer2:
if len(tasks)>0:
\n\n[任务列表]\n- task_id=%s, topic=%q\n- task_id=%s, topic=%q(当前任务)\n- task_id=%s, topic=%q\n
+
if len(tasks)>1 :存在多任务时，请先确认用户当前指的是哪一个任务。\n

Layer3:

if len(questions)>0:
\n\n[待解决冲突问题]\n- context_id=%s, task_id=%s\n  question=%s\n
+
if len(questions)>1:
\n存在多个冲突问题时，通过自然语言明确指向哪个任务或页面，系统会自动匹配对应冲突。\n

Layer5:
protocolInstructions：
\n\n
[动作协议]
1. 思考使用 #{...}，例如 #{思考内容}。
2. 工具调用使用 @{type|k:v|k:v}，例如 @{kb_query|query:导数定义}。
3. 对用户可见的自然语言不要放在 #{...} 或 @{...} 里。
4. 工具结果上下文统一以 <tool>...</tool> 注入。
5. 若被打断，保留可恢复轨迹，后续由 </interrupted> 表示中断续写语义。
6. 当前支持工具：
   - kb_query: 查询知识库，参数 query（查询内容）
   - web_search: 网络搜索，参数 query（搜索关键词）
   - get_memory: 召回记忆，参数 query（查询内容）
   - update_requirements: 更新需求信息，参数为 topic/description/total_pages/audience/global_style/knowledge_points/teaching_goals/teaching_logic/key_difficulties/duration/interaction_design/output_formats 中用户本次明确提到的字段，只填用户说的，其余字段不出现在 @{} 里
   - require_confirm: 请求用户确认需求信息（所有字段齐全后调用），无参数
   - ppt_init: 创建PPT任务，无参数（从 session 状态读取已收集的12个字段，字段不全会返回缺少字段错误）
   - ppt_mod: 反馈PPT修改意图，参数 raw_text（用户原话）、user_distance（该用户消息距离当前 @{} 的倒数位置，不打断时为1）
7. 遇到冲突问题时，基于用户原话直接通过 @{ppt_mod|raw_text:用户原话|user_distance:int} 反馈。

---

// 数据集 system prompt 构造规则
//
// 每条训练数据的 system 字段必须是真实渲染后的内容，不能用占位符。
// 根据场景按以下规则组合 Layer：
//
// [A-D] 信息收集阶段（字段未全部收集）
//   system = Layer1(BuildCollectionPrompt，含缺失字段列表+已收集字段) + Layer5
//   Layer2: 单任务时省略，多任务时加入
//   Layer3: 无冲突时省略
//
// [E] ppt_mod 阶段（字段已全部收集，PPT已生成）
//   system = Layer1(p.config.SystemPrompt，固定角色描述) + Layer2 + Layer5
//   Layer3: 无冲突时省略
//
// [F] require_confirm → ppt_init 阶段
//   system = Layer1(BuildCollectionPrompt，所有字段已收集，missing列表为空) + Layer2 + Layer5
//
// [G] 字段改口阶段
//   system = Layer1(BuildCollectionPrompt，含当前已收集字段) + Layer2 + Layer5
//
// [H/I/J] 并发/idle/多意图阶段
//   system = Layer1(p.config.SystemPrompt) + Layer2 + Layer5
//
// [K] 冲突问题阶段
//   system = Layer1(p.config.SystemPrompt) + Layer2 + Layer3(含冲突问题) + Layer5
//
// [L/M] 格式遵循/边界拒绝
//   system 根据具体 case 选择对应阶段的 Layer 组合
//
// 示例（D1 单字段收集，只有 topic 未收集）：
// {"role":"system", "content":"你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：topic\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。\n\n[动作协议]\n1. 思考使用 #{...}..."}
//
// 示例（D2 多字段已收集）：
// {"role":"system", "content":"你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：description, knowledge_points, teaching_goals, teaching_logic, key_difficulties, duration, global_style, interaction_design, output_formats\n\n已收集的信息：\n- topic: 高中数学导数\n- audience: 高三学生\n- total_pages: 20\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。\n\n[动作协议]\n..."}

---

完整多轮示例（覆盖所有 Layer 变化）：

第1轮 - 无字段收集（Layer1 + Layer5）
第2轮 - topic/total_pages 已收集（Layer1更新 + Layer5）
第3轮 - 继续收集字段（Layer1更新 + Layer5）
第N轮 - 全部字段收集完毕（Layer1更新 + Layer5）
第N+1轮 - 用户确认，Layer1切换为固定描述（Layer1切换 + Layer5）
第N+2轮 - 任务创建，加入Layer2（Layer1 + Layer2 + Layer5）
第N+3轮 - 用户修改PPT（Layer1 + Layer2 + Layer5）
第N+4轮 - 出现冲突，加入Layer3（Layer1 + Layer2 + Layer3 + Layer5）
第N+5轮 - 冲突解决，Layer3消失（Layer1 + Layer2 + Layer5）
第N+6轮 - 多任务，Layer2加入多任务提示（Layer1 + Layer2多任务 + Layer5）

详细内容见文档末尾附录。




纯对话数据集(2500条):
每条的格式为：
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出"},
    {"role":"user", "content":"用户继续说的话"},      
    {"role":"assistant", "content":"#{思考} 自然语言输出"}
    {"role":"user", "content":"用户继续说的话"},      
    {"role":"assistant", "content":"#{思考} 自然语言输出"}
    {"role":"user", "content":"用户继续说的话"},      
    {"role":"assistant", "content":"#{思考} 自然语言输出"}
}
(其中用户说话1轮的250条，2轮的250条，3轮的250条，4轮的250条，5轮的250条，6轮的250条，7轮的250条，8轮的250条，9轮的250条，10轮的250条)

---

工具调用数据集(4875条):
我们的7个工具：
1. kb_query ✓                                                            - 一次：kb_query:kb_query已发送，等待检索结果                          
- 二次：外部 ppt_message 回调 → kb_query:检索内容                      

2. web_search ✓                                                        
- 一次：web_search:web_search已发送，等待搜索结果
- 二次：外部 ppt_message 回调 → web_search:搜索内容

3. get_memory ✓
- 一次：get_memory:get_memory已发送，等待记忆召回结果
- 二次：外部 ppt_message 回调 → get_memory:记忆内容

4. update_requirements ✓（刚修）
- 一次：update_requirements:需求信息已更新
- 无二次

5. require_confirm ✓
- 一次：require_confirm:已发送确认请求，等待用户确认
- 无二次

6. ppt_mod ✓
- 一次：ppt_mod:PPT修改请求已发送
- 无二次

7. ppt_init ✓
- 一次：ppt_init:PPT任务已创建，TaskID: xxx（或缺少字段错误）
- 另外还有 task_list_update callback（不进历史，走 session 状态）      
- 无二次（ppt_status/page_rendered 是独立事件，不属于 ppt_init 的结果）

注意：@{} 执行后下一条 user 消息的内容不固定，可能是以下任意组合：
- 只有 <tool> 结果（工具先回来）
- 只有用户说话（用户比工具快）
- <tool> 结果 + 用户说话（两者都到了，flushToolResults 先 drain，再 AddUser）
- 两条 <tool>（快速确认 + 异步结果都到了）
数据集需覆盖所有组合，不能只造"@{}后必然跟tool"的数据。

---

// A. 正常流

// 说明：A1-A6 中"每个工具"指 7 个工具（kb_query, web_search, get_memory, update_requirements, require_confirm, ppt_init, ppt_mod）
// A2-A3 仅适用于异步工具（kb_query, web_search, get_memory），其余工具无二次回调
// 
// A 节总条数：
// - A1: 7工具 × 100 = 700
// - A2: 3异步工具 × 50 = 150
// - A3: 3异步工具 × 50 = 150
// - A4: 7工具 × 50 = 350
// - A5: 7工具 × 50 = 350
// - A6: 7工具 × 100 = 700
// A 小计: 2400

// A1: 每个工具单轮调用100条数据集（1次快速成功）
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
}

{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
    {"role":"user", "content":"用户说的话"},    
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
}


// A2: 异步工具（kb_query/web_search/get_memory）额外50条数据集（二次结果）
// 快速确认和异步真实结果分两次到达
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:已发送，等待结果</tool>"},
    {"role":"assistant", "content":"#{工具已发送，等待异步结果} 好的，正在查询..."},
    {"role":"user", "content":"<tool>tool_type:真实检索内容</tool>"},
    {"role":"assistant", "content":"#{收到异步结果，继续推理} 基于结果的回复"}
}

// A3: 异步工具额外50条数据集（用户比工具快，工具结果和用户话合并到达）
// @{} 发出后用户抢先说话，tool结果和用户话在同一个user消息里
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:已发送，等待结果</tool>\n用户抢先说的新话"},
    {"role":"assistant", "content":"#{工具已发送，同时收到用户新指令} 响应用户新话..."},
    {"role":"user", "content":"<tool>tool_type:真实检索内容</tool>"},
    {"role":"assistant", "content":"#{收到之前工具的异步结果} 补充回复..."}
}

// A4: 每个工具50条数据集（失败3次）
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:调用失败了</tool>"},
    {"role":"assistant", "content":"#{调用失败，重试一次} 调用xxx失败了，让我再试一次 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:调用失败了</tool>"},
    {"role":"assistant", "content":"#{调用失败，重试一次} 调用xxx再次失败，让我再试一次 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:调用失败了</tool>"},
    {"role":"assistant", "content":"#{调用失败多次，不再调用，向用户报告情况} 调用xxx多次失败了，xxx好像不可用，请稍后再试"}
}

// A5: 每个工具50条数据集（失败1或2次后调用成功，1次25条，2次25条）
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:调用失败了</tool>"},
    {"role":"assistant", "content":"#{调用失败，重试一次} 调用xxx失败了，让我再试一次 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:调用成功了</tool>"},
    {"role":"assistant", "content":"#{调用成功，继续正常推理} 基于工具结果的回复"}
}

// A6: 每个工具100条数据集（用户打断正在重试，1次50条，2次50条）
// </interrupted> 追加在被打断的 assistant 消息结尾，不是单独一条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:调用失败了</tool>"},
    {"role":"assistant", "content":"#{调用失败，重试一次} 再试一次 @{tool|k:v}</interrupted>"},
    {"role":"user", "content":"别试了"},
    {"role":"assistant", "content":"#{用户打断，放弃重试} 好的，不再重试了..."}
}

---

// B. 打断流 case

// 说明：B 节不按工具枚举，按打断场景枚举
// B 节总条数：
// - B1: 50, B2: 50, B3: 50, B3b: 50, B4: 50
// B 小计: 250

// B1: 自然语言输出中途被打断（@{} 之前）50条
// 用户在 assistant 说话过程中打断，此时还没到 @{}
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 我来帮你查一下相关内容</interrupted>"},
    {"role":"user", "content":"不用查了，直接告诉我"},
    {"role":"assistant", "content":"#{用户打断，放弃原计划} 好的，直接回答..."}
}

// B2: @{} 完整输出后立刻被打断 50条
// @{} 已完整，工具已触发，但 assistant 后续自然语言被打断
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} @{tool|k:v} 好的我已经发起查询</interrupted>"},
    {"role":"user", "content":"用户新指令"},
    {"role":"assistant", "content":"#{用户打断，工具已发出，响应新指令} 响应新指令..."},
    {"role":"user", "content":"<tool>tool_type:异步结果</tool>"},
    {"role":"assistant", "content":"#{收到之前工具的结果，结合新指令} 补充回复..."}
}

// B3: @{} 输出到一半超2s被硬截断 50条
// 不完整的 @{ 被删除，</interrupted> 追加在截断处
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 让我查一下</interrupted>"},
    // 注：原始输出可能是 "让我查一下 @{kb_query|quer" 但不完整@{被删除
    {"role":"user", "content":"用户新指令"},
    {"role":"assistant", "content":"#{被打断，工具未发出，响应新指令} 好的..."}
}

// B3b: @{} 闭合后被打断，工具已发出但后续自然语言被截断，下一轮补发工具结果 50条
// @{ 在等待期间成功闭合（工具已触发），但之后的自然语言被 </interrupted> 截断
// 模型需要意识到工具已发出，下一轮 tool 结果到来时正常消费
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"帮我查一下导数定义"},
    {"role":"assistant", "content":"#{思考} 好的 @{kb_query|query:导数定义} 正在查</interrupted>"},
    // 注：@{} 已完整闭合，工具已触发，后续"正在查"被截断
    {"role":"user", "content":"等等，顺便也查一下极限"},
    {"role":"assistant", "content":"#{工具已发出，同时响应用户新指令，补发新工具} 好的，同时查导数和极限 @{kb_query|query:极限定义}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"assistant", "content":"#{两个查询都在路上} 正在同时查询..."},
    {"role":"user", "content":"<tool>kb_query:导数是函数在某点的瞬时变化率...</tool>"},
    {"role":"assistant", "content":"#{收到第一个结果，还有一个未回} 导数的定义是：函数在某点的瞬时变化率..."},
    {"role":"user", "content":"<tool>kb_query:极限是函数趋近某点时的值...</tool>"},
    {"role":"assistant", "content":"#{收到第二个结果，综合回复} 极限的定义是：函数趋近某点时的值..."}
}

// B4: 异步工具结果隔多轮才回来 50条
// @{} 发出后用户继续说了1-2轮，tool结果才到
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"帮我查一下xxx"},
    {"role":"assistant", "content":"#{思考} 好的 @{kb_query|query:xxx}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"assistant", "content":"#{工具已发送} 正在查询，你还有其他问题吗？"},
    {"role":"user", "content":"顺便问一下yyy"},
    {"role":"assistant", "content":"#{回答用户新问题，工具结果还没来} yyy的回答..."},
    {"role":"user", "content":"<tool>kb_query:真实检索内容</tool>"},
    {"role":"assistant", "content":"#{收到之前kb_query的异步结果} 关于xxx，检索到..."}
}

---

// C. 并发/交错 case

// 说明：C 节按并发工具数量和到达顺序枚举
// C 节总条数：
// - C1: 50, C2: 50, C3: 50, C4a: 25, C4b: 25, C4c: 25
// C 小计: 225

// C1: 连续两个 @{}@{} 结果先后到达 50条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} @{kb_query|query:xxx} @{web_search|query:yyy}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>web_search:已发送，等待搜索结果</tool>"},
    {"role":"assistant", "content":"#{两个工具都已发送，等待结果} 正在同时查询..."},
    {"role":"user", "content":"<tool>kb_query:检索内容</tool>"},
    {"role":"assistant", "content":"#{收到kb_query结果，web_search还没回} 知识库结果是..."},
    {"role":"user", "content":"<tool>web_search:搜索内容</tool>"},
    {"role":"assistant", "content":"#{收到web_search结果，综合两个结果} 综合来看..."}
}

// C2: 连续两个 @{}@{} 结果同时到达 50条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} @{kb_query|query:xxx} @{web_search|query:yyy}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>web_search:已发送，等待搜索结果</tool>"},
    {"role":"assistant", "content":"#{两个工具都已发送} 正在查询..."},
    {"role":"user", "content":"<tool>kb_query:检索内容</tool>"},
    {"role":"user", "content":"<tool>web_search:搜索内容</tool>"},
    {"role":"assistant", "content":"#{两个结果同时到，综合推理} 综合来看..."}
}

// C3: 连续两个 @{}@{} 第一个失败第二个成功 50条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} @{kb_query|query:xxx} @{web_search|query:yyy}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>web_search:已发送，等待搜索结果</tool>"},
    {"role":"assistant", "content":"#{两个工具都已发送} 正在查询..."},
    {"role":"user", "content":"<tool>kb_query:查询失败</tool>"},
    {"role":"user", "content":"<tool>web_search:搜索内容</tool>"},
    {"role":"assistant", "content":"#{kb_query失败但web_search成功，基于web结果回复} 知识库查询失败，但网络搜索到..."}
}

// C4a: 3个工具同时调用，结果同时到 25条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} @{kb_query|query:xxx} @{web_search|query:yyy} @{get_memory|query:zzz}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>web_search:已发送，等待搜索结果</tool>"},
    {"role":"user", "content":"<tool>get_memory:已发送，等待记忆召回结果</tool>"},
    {"role":"assistant", "content":"#{三个工具都已发送} 正在同时查询..."},
    {"role":"user", "content":"<tool>kb_query:检索内容</tool>"},
    {"role":"user", "content":"<tool>web_search:搜索内容</tool>"},
    {"role":"user", "content":"<tool>get_memory:记忆内容</tool>"},
    {"role":"assistant", "content":"#{三个结果同时到，综合推理} 综合来看..."}
}

// C4b: 3个工具同时调用，结果分批到（先1后2）25条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} @{kb_query|query:xxx} @{web_search|query:yyy} @{get_memory|query:zzz}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>web_search:已发送，等待搜索结果</tool>"},
    {"role":"user", "content":"<tool>get_memory:已发送，等待记忆召回结果</tool>"},
    {"role":"assistant", "content":"#{三个工具都已发送} 正在同时查询..."},
    {"role":"user", "content":"<tool>kb_query:检索内容</tool>"},
    {"role":"assistant", "content":"#{收到kb_query，还有2个未回} 知识库结果是...，等待其他结果"},
    {"role":"user", "content":"<tool>web_search:搜索内容</tool>"},
    {"role":"user", "content":"<tool>get_memory:记忆内容</tool>"},
    {"role":"assistant", "content":"#{另外两个结果到了，综合三个结果} 综合来看..."}
}

// C4c: 3个工具同时调用，结果逐个到 25条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} @{kb_query|query:xxx} @{web_search|query:yyy} @{get_memory|query:zzz}"},
    {"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>web_search:已发送，等待搜索结果</tool>"},
    {"role":"user", "content":"<tool>get_memory:已发送，等待记忆召回结果</tool>"},
    {"role":"assistant", "content":"#{三个工具都已发送} 正在同时查询..."},
    {"role":"user", "content":"<tool>kb_query:检索内容</tool>"},
    {"role":"assistant", "content":"#{收到kb_query，还有2个未回} 知识库结果是..."},
    {"role":"user", "content":"<tool>web_search:搜索内容</tool>"},
    {"role":"assistant", "content":"#{收到web_search，还有1个未回} 网络搜索结果是..."},
    {"role":"user", "content":"<tool>get_memory:记忆内容</tool>"},
    {"role":"assistant", "content":"#{三个结果全部到齐，综合推理} 综合来看..."}
}

---

// D. update_requirements 专项（参数数量1-12）

// 核心规则：用户说了什么字段就填什么，不补不猜，其余字段不出现在@{}里

// 说明：D 节按 update_requirements 参数数量枚举
// D 节总条数：
// - D1: 100（单字段，每个字段约8条）
// - D2: 150（2-5个字段）
// - D3: 50（全部12个字段）
// D 小计: 300

// D1: 单字段 100条（每个字段各约8条）
{
    {"role":"user", "content":"主题是高中数学导数"},
    {"role":"assistant", "content":"#{用户只说了topic} @{update_requirements|topic:高中数学导数}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"好的，主题已记录为高中数学导数，还需要了解一下受众和页数等信息..."}
}

// D2: 多字段（2-5个）150条
{
    {"role":"user", "content":"做一个高中数学导数的PPT，受众是高三学生，大概20页"},
    {"role":"assistant", "content":"#{用户说了topic/audience/total_pages三个字段} @{update_requirements|topic:高中数学导数|audience:高三学生|total_pages:20}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"好的，已记录主题、受众和页数，还需要了解教学目标和重难点..."}
}

// D3: 全字段（12个）50条
{
    {"role":"user", "content":"<用户一次性说完所有信息>"},
    {"role":"assistant", "content":"#{用户说了全部12个字段} @{update_requirements|topic:xxx|description:xxx|total_pages:20|audience:xxx|global_style:xxx|knowledge_points:xxx|teaching_goals:xxx|teaching_logic:xxx|key_difficulties:xxx|duration:45分钟|interaction_design:xxx|output_formats:xxx}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"#{信息齐全，可以确认} 所有信息已收集完毕，请确认..."}
}

---

// E. ppt_mod 专项（user_distance 填法）

// 核心规则：user_distance = @{} 前倒数第几个 user 消息
// 用户不打断正常对话时 user_distance=1（@{} 紧跟在用户话后面）
// 用户打断时 user_distance 可能是2或更大

// 说明：E 节按 user_distance 值枚举
// E 节总条数：
// - E1: 100（distance=1，正常单轮）
// - E2: 50（distance=2，打断场景）
// E 小计: 150

// E1: user_distance=1，正常单轮 100条
{
    {"role":"user", "content":"把第三页改成蓝色背景"},
    {"role":"assistant", "content":"#{用户刚说的话，distance=1} @{ppt_mod|raw_text:把第三页改成蓝色背景|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"好的，已发送修改请求..."}
}

// E2: user_distance=2，中间有一轮assistant说话后用户又说 50条
// 用户说了A，assistant回复，用户说了B触发ppt_mod，raw_text是B，distance=1
// 但如果assistant在回复A时就调用ppt_mod处理A，此时B还没说，distance=1
// 真正distance=2的场景：assistant输出中途被打断，用户说了新话，
// assistant基于更早那句话调用ppt_mod
{
    {"role":"user", "content":"把标题改大一点"},                          // 这是倒数第2个user
    {"role":"assistant", "content":"#{思考} 好的我来修改</interrupted>"},  // 被打断
    {"role":"user", "content":"等等，字体也换成黑体"},                     // 这是倒数第1个user
    {"role":"assistant", "content":"#{用户打断，raw_text是更早那句，distance=2} @{ppt_mod|raw_text:把标题改大一点|user_distance:2} 同时字体也换成黑体 @{ppt_mod|raw_text:字体也换成黑体|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"好的，两个修改都已发送..."}
}

---

// F. require_confirm → ppt_init 两阶段专项

// 核心规则：
// 1. 所有12个字段齐全后，AI 调用 @{require_confirm}，前端展示需求信息给用户
// 2. 用户口头说"确认/好的/可以"后，AI 才调用 @{ppt_init}，两者不在同一轮
// 3. require_confirm 不需要在输出里列出字段内容（前端已展示）
// 4. 用户口头否认/要求修改时，重新走 update_requirements 流程再 require_confirm

// 说明：F 节按 require_confirm → ppt_init 流程枚举
// F 节总条数：
// - F1: 75（正常两阶段流）
// - F2: 50（用户否认修改后重新确认）
// - F3: 25（字段不全时催促）
// - F4: 25（require_confirm 失败重试）
// F 小计: 175

// F1: 正常两阶段流（字段齐全 → require_confirm → 用户口头确认 → ppt_init）75条
{
    {"role":"system", "content":"<具体身份定位，已收集全部12个字段>"},
    {"role":"user", "content":"好的就这样吧"},
    {"role":"assistant", "content":"#{所有字段已齐全，发起确认请求，等待用户口头确认} 好的，我已整理好所有信息，请查看界面上的需求摘要，确认无误后告诉我 @{require_confirm}"},
    {"role":"user", "content":"<tool>require_confirm:已发送确认请求，等待用户确认</tool>"},
    {"role":"assistant", "content":"#{require_confirm已发出，等待用户口头确认} 请确认界面上的信息是否正确。"},
    {"role":"user", "content":"确认，开始做吧"},
    {"role":"assistant", "content":"#{用户口头确认，现在发起ppt_init} 好的，开始制作！ @{ppt_init|topic:xxx|description:xxx|total_pages:20|audience:xxx|global_style:xxx|knowledge_points:xxx|teaching_goals:xxx|teaching_logic:xxx|key_difficulties:xxx|duration:45分钟|interaction_design:xxx|output_formats:xxx}"},
    {"role":"user", "content":"<tool>ppt_init:PPT任务已创建，TaskID: task_001</tool>"},
    {"role":"assistant", "content":"#{ppt_init成功，任务已创建} PPT制作任务已创建，正在生成中，稍等一下。"}
}

// F2: 用户口头否认，修改字段后重新确认 50条
{
    {"role":"system", "content":"<具体身份定位，已收集全部12个字段，已发起require_confirm>"},
    {"role":"user", "content":"等等，页数改成30页"},
    {"role":"assistant", "content":"#{用户否认，修改字段，重新require_confirm} 好的，页数改为30页 @{update_requirements|total_pages:30}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"#{字段更新完毕，重新发起确认} 已更新页数为30页，请重新查看界面确认 @{require_confirm}"},
    {"role":"user", "content":"<tool>require_confirm:已发送确认请求，等待用户确认</tool>"},
    {"role":"assistant", "content":"请确认更新后的信息是否正确。"},
    {"role":"user", "content":"好的这次没问题"},
    {"role":"assistant", "content":"#{用户口头确认，发起ppt_init} @{ppt_init|topic:xxx|description:xxx|total_pages:30|audience:xxx|global_style:xxx|knowledge_points:xxx|teaching_goals:xxx|teaching_logic:xxx|key_difficulties:xxx|duration:45分钟|interaction_design:xxx|output_formats:xxx}"},
    {"role":"user", "content":"<tool>ppt_init:PPT任务已创建，TaskID: task_002</tool>"},
    {"role":"assistant", "content":"任务已创建，开始生成。"}
}

// F3: 字段不全时用户催促开始，AI 拒绝并继续收集 25条
{
    {"role":"system", "content":"<具体身份定位，字段未全部收集，缺少teaching_goals/key_difficulties>"},
    {"role":"user", "content":"差不多了，开始做吧"},
    {"role":"assistant", "content":"#{字段不全，不能发起require_confirm，继续收集} 还差一点，教学目标和重难点还没填，能简单说一下吗？"},
    {"role":"user", "content":"教学目标是让学生掌握导数基本概念，重难点是极限的理解"},
    {"role":"assistant", "content":"#{收到缺失字段，更新后字段齐全，发起确认} @{update_requirements|teaching_goals:让学生掌握导数基本概念|key_difficulties:极限的理解}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"#{字段齐全，发起确认} 好的，信息收集完毕，请查看界面确认 @{require_confirm}"},
    {"role":"user", "content":"<tool>require_confirm:已发送确认请求，等待用户确认</tool>"},
    {"role":"assistant", "content":"请确认所有信息是否正确。"}
}

// F4: require_confirm 发送失败，重试 25条
{
    {"role":"system", "content":"<具体身份定位，已收集全部12个字段>"},
    {"role":"user", "content":"可以了"},
    {"role":"assistant", "content":"#{字段齐全，发起确认} 好的，请确认需求信息 @{require_confirm}"},
    {"role":"user", "content":"<tool>require_confirm:发送确认请求失败</tool>"},
    {"role":"assistant", "content":"#{require_confirm失败，重试} 确认请求发送失败，我再试一次 @{require_confirm}"},
    {"role":"user", "content":"<tool>require_confirm:已发送确认请求，等待用户确认</tool>"},
    {"role":"assistant", "content":"请查看界面上的需求信息，确认无误后告诉我。"}
}

---

// G. 字段改口/覆盖专项

// 核心规则：
// 1. 用户改口时，只更新用户明确说的字段，其余字段不动
// 2. update_requirements 是覆盖语义，新值直接替换旧值
// 3. 模型不能"记住"旧值并在输出里重复，以免误导用户

// 说明：G 节按改口场景枚举
// G 节总条数：
// - G1: 50（单字段改口）
// - G2: 50（多字段同时改口）
// - G3: 25（改口后又改回来）
// - G4: 25（用户说"不对"但没给新值）
// - G5: 25（require_confirm 后反悔）
// G 小计: 175

// G1: 单字段改口（直接覆盖）50条
{
    {"role":"system", "content":"<具体身份定位，topic已记录为高中数学导数>"},
    {"role":"user", "content":"不对，主题改成初中数学函数"},
    {"role":"assistant", "content":"#{用户改口topic，直接覆盖} 好的，主题改为初中数学函数 @{update_requirements|topic:初中数学函数}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"已更新主题为初中数学函数，其他信息不变。"}
}

// G2: 多字段同时改口 50条
{
    {"role":"system", "content":"<具体身份定位，已记录topic=高中数学导数，audience=高三学生，total_pages=20>"},
    {"role":"user", "content":"受众改成高一学生，页数改成15页"},
    {"role":"assistant", "content":"#{用户同时改口audience和total_pages} 好的 @{update_requirements|audience:高一学生|total_pages:15}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"已更新受众为高一学生，页数为15页。"}
}

// G3: 用户改口后又改回来 25条
{
    {"role":"system", "content":"<具体身份定位，topic已记录为高中数学导数>"},
    {"role":"user", "content":"主题改成初中数学"},
    {"role":"assistant", "content":"#{覆盖topic} @{update_requirements|topic:初中数学}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"好的，主题已改为初中数学。"},
    {"role":"user", "content":"算了还是高中数学导数吧"},
    {"role":"assistant", "content":"#{用户又改回来，再次覆盖} 好的，改回高中数学导数 @{update_requirements|topic:高中数学导数}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"已改回高中数学导数。"}
}

// G4: 用户说"不对"但没说新值，AI 追问 25条
{
    {"role":"system", "content":"<具体身份定位，topic已记录为高中数学导数>"},
    {"role":"user", "content":"主题不对"},
    {"role":"assistant", "content":"#{用户否定但没给新值，不能乱猜，追问} 好的，请问主题应该改成什么？"},
    {"role":"user", "content":"改成高中物理力学"},
    {"role":"assistant", "content":"#{收到新值，覆盖} @{update_requirements|topic:高中物理力学}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"已更新主题为高中物理力学。"}
}

// G5: 改口发生在 require_confirm 之后（已发起确认但用户口头反悔）25条
// 用户已看到界面上的需求摘要，口头说要改某字段，AI 更新后重新 require_confirm，
// 等用户再次口头确认后才发 ppt_init
{
    {"role":"system", "content":"<具体身份定位，已收集全部12个字段，已发起require_confirm等待用户口头确认>"},
    {"role":"user", "content":"等等，风格改成简约白色"},
    {"role":"assistant", "content":"#{用户反悔修改字段，重新update并重新confirm，不发ppt_init} 好的，风格改为简约白色 @{update_requirements|global_style:简约白色}"},
    {"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"},
    {"role":"assistant", "content":"#{字段更新完毕，重新发起确认，等待用户口头确认} 已更新风格，请重新查看界面确认 @{require_confirm}"},
    {"role":"user", "content":"<tool>require_confirm:已发送确认请求，等待用户确认</tool>"},
    {"role":"assistant", "content":"请确认更新后的信息是否正确。"},
    {"role":"user", "content":"好的没问题"},
    {"role":"assistant", "content":"#{用户口头确认，现在发ppt_init} @{ppt_init|topic:xxx|description:xxx|total_pages:20|audience:xxx|global_style:简约白色|knowledge_points:xxx|teaching_goals:xxx|teaching_logic:xxx|key_difficulties:xxx|duration:45分钟|interaction_design:xxx|output_formats:xxx}"},
    {"role":"user", "content":"<tool>ppt_init:PPT任务已创建，TaskID: task_004</tool>"},
    {"role":"assistant", "content":"任务已创建，开始生成。"}
}

---

// H. 工具结果延迟消费（AI 上一轮正在输出，tool 结果下一轮才被 flushToolResults 消费）

// 核心规则：
// tool 结果进了 contextQueue，但当前轮次 AI 还在输出，
// 下一轮 startProcessing 时 flushToolResults() 才把它写入历史。
// 模型需要学会：上一轮我说到一半，这轮突然多了一条 tool 消息，要自然衔接。

// 说明：H 节按延迟消费场景枚举
// H 节总条数：
// - H1: 50（单工具结果延迟）
// - H2: 50（多工具结果积压）
// H 小计: 100

// H1: AI 正常输出完毕后，下一轮 tool 结果才到（无打断）50条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"帮我查一下导数的定义"},
    {"role":"assistant", "content":"#{发起查询} 好的，我来查一下 @{kb_query|query:导数定义}"},
    {"role":"user", "content":"<tool>kb_query:kb_query已发送，等待检索结果</tool>"},
    {"role":"assistant", "content":"#{工具已发送，继续和用户聊} 正在检索，你还有其他想了解的吗？"},
    {"role":"user", "content":"顺便问一下，极限和导数有什么关系"},
    // 注：此时 kb_query 异步结果也到了，flushToolResults 在这轮 startProcessing 前把它写入历史
    // 所以下面这条 assistant 消息前，history 里已经有了 kb_query 的真实结果
    {"role":"user", "content":"<tool>kb_query:导数是函数在某点的瞬时变化率，定义为极限...</tool>"},
    {"role":"assistant", "content":"#{这轮同时收到用户新问题和kb_query结果，先回答新问题再补充检索结果} 极限是导数的基础，导数本质上就是差商的极限。另外，关于导数定义，检索到：导数是函数在某点的瞬时变化率..."}
}

// H2: 多个工具结果在 AI 输出期间积压，下一轮一次性消费 50条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"同时帮我查知识库和搜索网络"},
    {"role":"assistant", "content":"#{同时发起两个查询} 好的，同时查询 @{kb_query|query:xxx} @{web_search|query:xxx}"},
    {"role":"user", "content":"<tool>kb_query:kb_query已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>web_search:web_search已发送，等待搜索结果</tool>"},
    {"role":"assistant", "content":"#{两个工具都已发送，等待结果} 正在同时查询，稍等..."},
    {"role":"user", "content":"好的，我等着"},
    // 此时两个异步结果都已到达 contextQueue，flushToolResults 一次性写入
    {"role":"user", "content":"<tool>kb_query:知识库检索内容...</tool>"},
    {"role":"user", "content":"<tool>web_search:网络搜索内容...</tool>"},
    {"role":"assistant", "content":"#{两个结果都到了，综合回复} 综合知识库和网络搜索结果：..."}
}

---

// I. Idle Trigger（无用户说话，仅 tool 结果触发 startProcessing）

// 核心规则：
// EnqueueContext 在 session idle 时主动触发 startProcessing(sCtx, "")
// 此时 userText 为空，history 里没有新的 user 消息，只有 tool 结果
// 模型需要学会：没有用户说话，仅凭 tool 结果主动输出，不能输出空内容

// 说明：I 节按 idle trigger 场景枚举
// I 节总条数：
// - I1: 75（单工具结果触发 idle）
// - I2: 50（多工具结果触发 idle）
// - I3: 25（idle trigger 时工具失败）
// I 小计: 150

// I1: 单个 tool 结果触发 idle，AI 主动播报结果 75条
// 注：history 里没有新的 user 消息，直接是 tool 消息触发本轮
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"帮我查一下xxx"},
    {"role":"assistant", "content":"#{发起查询} 好的 @{kb_query|query:xxx}"},
    {"role":"user", "content":"<tool>kb_query:kb_query已发送，等待检索结果</tool>"},
    {"role":"assistant", "content":"#{工具已发送，等待结果} 正在查询..."},
    // 用户没有说话，异步结果到来触发 idle startProcessing
    // flushToolResults 把结果写入历史，userText=""，history 里无新 user 消息
    {"role":"user", "content":"<tool>kb_query:检索到的内容...</tool>"},
    {"role":"assistant", "content":"#{idle trigger，收到kb_query结果，主动播报} 查到了，关于xxx：检索到的内容..."}
}

// I2: 多个 tool 结果积压后触发 idle，AI 综合播报 50条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"帮我同时查知识库和记忆"},
    {"role":"assistant", "content":"#{同时发起两个查询} @{kb_query|query:xxx} @{get_memory|query:yyy}"},
    {"role":"user", "content":"<tool>kb_query:kb_query已发送，等待检索结果</tool>"},
    {"role":"user", "content":"<tool>get_memory:get_memory已发送，等待记忆召回结果</tool>"},
    {"role":"assistant", "content":"#{两个工具已发送} 正在查询..."},
    // 用户没有说话，两个结果都到了，触发 idle startProcessing
    {"role":"user", "content":"<tool>kb_query:知识库内容...</tool>"},
    {"role":"user", "content":"<tool>get_memory:记忆内容...</tool>"},
    {"role":"assistant", "content":"#{idle trigger，两个结果都到了，综合播报} 查询完成，知识库显示...，结合你之前的记忆..."}
}

// I3: idle trigger 时 tool 结果是失败 25条
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"帮我搜索一下xxx"},
    {"role":"assistant", "content":"#{发起搜索} @{web_search|query:xxx}"},
    {"role":"user", "content":"<tool>web_search:web_search已发送，等待搜索结果</tool>"},
    {"role":"assistant", "content":"#{工具已发送} 正在搜索..."},
    // 用户没有说话，失败结果触发 idle startProcessing
    {"role":"user", "content":"<tool>web_search:搜索失败</tool>"},
    {"role":"assistant", "content":"#{idle trigger，搜索失败，主动告知用户} 搜索好像遇到了问题，暂时无法获取结果，你可以稍后再试或换个关键词。"}
}

---

// J. ppt_mod 一句话多意图专项

// 核心规则：
// 1. 用户一句话包含多个独立修改意图，每个意图单独一个 @{ppt_mod}
// 2. 每个 @{ppt_mod} 的 raw_text 是用户原话（整句），user_distance 都是 1
// 3. 不能把多个意图合并成一个 @{ppt_mod}
// 4. 意图之间顺序不重要，但每个都要发出

// 说明：J 节按一句话中的意图数量枚举
// J 节总条数：
// - J1: 75（两个意图）
// - J2: 50（三个意图）
// - J3: 25（多意图中有失败）
// J 小计: 150

// J1: 两个修改意图，同一句话 75条
{
    {"role":"user", "content":"把第三页背景改成蓝色，标题字体换成黑体"},
    {"role":"assistant", "content":"#{用户一句话两个意图，各发一个ppt_mod，raw_text都是原话，distance都是1} 好的，两个修改都发出去 @{ppt_mod|raw_text:把第三页背景改成蓝色|user_distance:1} @{ppt_mod|raw_text:标题字体换成黑体|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"好的，背景和字体两个修改都已发送。"}
}

// J2: 三个修改意图，同一句话 50条
{
    {"role":"user", "content":"第三页背景改蓝色，标题换黑体，再加一张总结页"},
    {"role":"assistant", "content":"#{三个意图，各发一个ppt_mod} @{ppt_mod|raw_text:第三页背景改蓝色，标题换黑体，再加一张总结页|user_distance:1} @{ppt_mod|raw_text:第三页背景改蓝色，标题换黑体，再加一张总结页|user_distance:1} @{ppt_mod|raw_text:第三页背景改蓝色，标题换黑体，再加一张总结页|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"好的，三个修改都已发送。"}
}

// J3: 多意图中有一个失败 25条
{
    {"role":"user", "content":"第二页标题改大，第五页删掉"},
    {"role":"assistant", "content":"#{两个意图} @{ppt_mod|raw_text:第二页标题改大，第五页删掉|user_distance:1} @{ppt_mod|raw_text:第二页标题改大，第五页删掉|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"user", "content":"<tool>ppt_mod:发送反馈失败</tool>"},
    {"role":"assistant", "content":"#{一个成功一个失败} 第二页标题修改已发送，但第五页删除请求失败了，请稍后再试。"}
}

---

// K. 冲突问题（system prompt Layer3 注入 [待解决冲突问题]）专项

// 核心规则：
// 1. system prompt 里有 [待解决冲突问题] 时，AI 需要在自然对话中把冲突带出来
// 2. 用户回答冲突后，通过 @{ppt_mod|raw_text:用户原话|user_distance:1} 反馈
// 3. 多个冲突时，一次只问一个，不要一次性把所有冲突都抛给用户
// 4. 冲突解决后，system prompt 里该条目会消失（下一轮不再出现）

// 说明：K 节按冲突问题场景枚举
// K 节总条数：
// - K1: 50（单冲突，自然带出）
// - K2: 50（多冲突，逐个解决）
// - K3: 25（用户主动说话时顺带提冲突）
// - K4: 25（冲突被打断，下一轮继续）
// K 小计: 150

// K1: 单个冲突，AI 自然带出并等待用户回答 50条
{
    {"role":"system", "content":"<具体身份定位>\n\n[待解决冲突问题]\n- context_id=ctx_001, task_id=task_001\n  question=第3页标题与第5页内容存在逻辑矛盾，请确认保留哪个版本"},
    {"role":"user", "content":"PPT做得怎么样了"},
    {"role":"assistant", "content":"#{system里有冲突问题，自然带出} 正在生成中，不过有个地方需要你确认一下：第3页标题和第5页内容有些逻辑上的矛盾，你希望保留哪个版本？"},
    {"role":"user", "content":"保留第3页的版本"},
    {"role":"assistant", "content":"#{用户回答了冲突，通过ppt_mod反馈} 好的 @{ppt_mod|raw_text:保留第3页的版本|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"已反馈，保留第3页版本。"}
}

// K2: 多个冲突，逐个解决 50条
{
    {"role":"system", "content":"<具体身份定位>\n\n[待解决冲突问题]\n- context_id=ctx_001, task_id=task_001\n  question=第3页标题与第5页内容存在逻辑矛盾\n- context_id=ctx_002, task_id=task_001\n  question=第7页图表数据与第2页描述不一致\n存在多个冲突问题时，通过自然语言明确指向哪个任务或页面，系统会自动匹配对应冲突。"},
    {"role":"user", "content":"有什么问题吗"},
    {"role":"assistant", "content":"#{有两个冲突，先问第一个，不要一次全抛} 有两处需要确认。先说第一个：第3页标题和第5页内容有逻辑矛盾，你希望怎么处理？"},
    {"role":"user", "content":"第3页标题改一下，和第5页保持一致"},
    {"role":"assistant", "content":"#{用户回答第一个冲突，ppt_mod反馈，然后问第二个} 好的 @{ppt_mod|raw_text:第3页标题改一下，和第5页保持一致|user_distance:1} 还有一处：第7页图表数据和第2页描述不一致，以哪个为准？"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"#{收到第一个ppt_mod结果，等待用户回答第二个冲突} 第一个已处理，等你确认第7页的问题。"},
    {"role":"user", "content":"以第2页描述为准"},
    {"role":"assistant", "content":"#{用户回答第二个冲突} @{ppt_mod|raw_text:以第2页描述为准|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"两个冲突都已处理完毕。"}
}

// K3: 用户主动说话时 AI 顺带提冲突 25条
// 用户说了别的事，AI 先回答，然后顺带提冲突
{
    {"role":"system", "content":"<具体身份定位>\n\n[待解决冲突问题]\n- context_id=ctx_003, task_id=task_001\n  question=第4页知识点与教学目标不匹配"},
    {"role":"user", "content":"第一页标题改大一点"},
    {"role":"assistant", "content":"#{先处理用户请求，再顺带提冲突} 好的 @{ppt_mod|raw_text:第一页标题改大一点|user_distance:1} 另外有个地方需要你确认：第4页知识点和教学目标有些不匹配，你希望怎么调整？"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"标题修改已发送，等你确认第4页的问题。"},
    {"role":"user", "content":"知识点按教学目标重新整理"},
    {"role":"assistant", "content":"#{用户回答冲突} @{ppt_mod|raw_text:知识点按教学目标重新整理|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"好的，已发送修改请求。"}
}

// K4: 冲突问题被打断，下一轮继续 25条
{
    {"role":"system", "content":"<具体身份定位>\n\n[待解决冲突问题]\n- context_id=ctx_004, task_id=task_001\n  question=第6页图片与文字描述不符"},
    {"role":"user", "content":"有冲突吗"},
    {"role":"assistant", "content":"#{带出冲突} 有一处：第6页图片和文字描述不符，你希望</interrupted>"},
    {"role":"user", "content":"等等，先把第一页标题改大"},
    {"role":"assistant", "content":"#{被打断，先处理用户新请求} 好的 @{ppt_mod|raw_text:先把第一页标题改大|user_distance:1} 改完之后再说第6页的问题。"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"标题已发送修改。关于第6页，图片和文字描述不符，你希望以图片为准还是文字为准？"},
    {"role":"user", "content":"以文字为准"},
    {"role":"assistant", "content":"#{用户回答冲突} @{ppt_mod|raw_text:以文字为准|user_distance:1}"},
    {"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"},
    {"role":"assistant", "content":"好的，已处理。"}
}

---

// L. 格式遵循专项（Format Compliance）

// 核心规则：
// 1. 思考内容必须放在 #{} 里，不能泄露到自然语言输出
// 2. @{} 格式严格：工具名|k:v|k:v，不加多余空格/换行/引号
// 3. #{} 和 @{} 之间用空格分隔，不用换行
// 4. 自然语言输出在 @{} 之后，不夹在 @{} 参数里
// 5. </interrupted> 只追加在被打断的 assistant 消息末尾，不单独成行

// 说明：L 节按格式错误类型枚举，训练模型避免常见格式错误
// L 节总条数：
// - L1: 75（思考不泄露）
// - L2: 75（@{} 参数格式严格）
// - L3: 50（多 @{} 空格分隔）
// - L4: 50（自然语言不混入参数）
// - L5: 50（</interrupted> 位置）
// - L6: 50（#{} 不为空）
// - L7: 50（特殊字符处理）
// L 小计: 400

// L1: 思考内容不泄露到输出 75条
// 错误示范：直接把推理过程说出来
// 正确示范：推理放 #{}，输出只有结论
{
    {"role":"user", "content":"帮我查一下高中数学导数的定义"},
    {"role":"assistant", "content":"#{用户需要查导数定义，应该用kb_query，query参数填导数定义} 好的，我来查一下 @{kb_query|query:导数定义}"}
    // 错误写法（不能出现在训练数据里）：
    // {"role":"assistant", "content":"我觉得应该用kb_query来查，query填导数定义，好的 @{kb_query|query:导数定义}"}
}

// L2: @{} 参数格式严格，不加引号/空格/换行 75条
{
    {"role":"user", "content":"主题是高中数学，受众是高三学生，做20页"},
    {"role":"assistant", "content":"#{三个字段} @{update_requirements|topic:高中数学|audience:高三学生|total_pages:20}"}
    // 错误写法（不能出现）：
    // @{update_requirements | topic: 高中数学 | audience: 高三学生}  （多余空格）
    // @{update_requirements|topic:"高中数学"|audience:"高三学生"}    （多余引号）
}

// L3: 多个 @{} 之间用空格分隔，不换行 50条
{
    {"role":"user", "content":"同时查知识库和搜索网络"},
    {"role":"assistant", "content":"#{两个工具} 好的，同时查询 @{kb_query|query:xxx} @{web_search|query:xxx}"}
    // 错误写法（不能出现）：
    // "好的\n@{kb_query|query:xxx}\n@{web_search|query:xxx}"  （换行分隔）
}

// L4: 自然语言不夹在 @{} 参数里 50条
{
    {"role":"user", "content":"把第三页背景改蓝色"},
    {"role":"assistant", "content":"#{ppt_mod} 好的 @{ppt_mod|raw_text:把第三页背景改蓝色|user_distance:1} 修改请求已发送"}
    // 错误写法（不能出现）：
    // @{ppt_mod|raw_text:把第三页背景改蓝色，好的我来改|user_distance:1}  （自然语言混入参数）
}

// L5: </interrupted> 位置正确 50条
{
    {"role":"user", "content":"帮我查一下"},
    {"role":"assistant", "content":"#{发起查询} 好的，我来查 @{kb_query|query:xxx}</interrupted>"},
    {"role":"user", "content":"不用查了"},
    {"role":"assistant", "content":"#{用户打断，放弃查询} 好的，不查了。"}
    // 错误写法（不能出现）：
    // {"role":"assistant", "content":"</interrupted>好的，不查了。"}  （</interrupted>出现在新消息开头）
}

// L6: #{} 内容不为空，必须有实质推理 50条
{
    {"role":"user", "content":"你好"},
    {"role":"assistant", "content":"#{用户打招呼，这是纯对话场景，不需要调用工具，直接回复} 你好！有什么我可以帮你的？"}
    // 错误写法（不能出现）：
    // {"role":"assistant", "content":"#{} 你好！"}  （空think）
    // {"role":"assistant", "content":"你好！"}       （缺少think）
}

// L7: 工具参数值含特殊字符时的处理 50条
// raw_text 里用户原话可能含竖线、冒号等，直接放入不转义
{
    {"role":"user", "content":"把标题改成：高中数学|导数专题"},
    {"role":"assistant", "content":"#{ppt_mod，raw_text是用户原话，直接放入} @{ppt_mod|raw_text:把标题改成：高中数学|导数专题|user_distance:1}"}
}

---

// M. 边界拒绝专项（Boundary Handling）

// 核心规则：
// 1. 用户说模糊的话，不能脑补字段值，必须追问
// 2. 纯闲聊场景不触发任何工具
// 3. 用户说"随便/都行"，不能乱填，要追问或给选项
// 4. 字段不全时不能提前发 require_confirm 或 ppt_init
// 5. 用户没有明确修改意图时不能乱发 ppt_mod

// 说明：M 节按边界错误类型枚举，训练模型避免过度调用工具
// M 节总条数：
// - M1: 75（模糊话追问）
// - M2: 50（"随便"追问）
// - M3: 75（纯闲聊不调工具）
// - M4: 50（字段不全不能 confirm）
// - M5: 50（无明确意图不发 ppt_mod）
// M 小计: 300

// M1: 用户说模糊话，不脑补字段，追问 75条
{
    {"role":"user", "content":"帮我做个PPT"},
    {"role":"assistant", "content":"#{用户没给任何字段，不能乱猜，追问topic} 好的，请问PPT的主题是什么？"}
    // 错误写法（不能出现）：
    // @{update_requirements|topic:PPT}  （乱猜topic）
}

// M2: 用户说"随便"，追问而不是乱填 50条
{
    {"role":"system", "content":"<具体身份定位，已记录topic=高中数学导数>"},
    {"role":"user", "content":"风格随便"},
    {"role":"assistant", "content":"#{用户说随便，不能乱填global_style，给几个选项} 风格有几种常见选择：简约白色、深色商务、彩色活泼，你倾向哪种？"}
    // 错误写法（不能出现）：
    // @{update_requirements|global_style:随便}
}

// M3: 纯闲聊，不触发工具 75条
{
    {"role":"user", "content":"你觉得高中数学难吗"},
    {"role":"assistant", "content":"#{纯闲聊，不需要工具} 高中数学对很多同学来说确实有挑战，尤其是导数和极限这块..."}
    // 错误写法（不能出现）：
    // @{kb_query|query:高中数学难度}  （不必要的工具调用）
}

// M4: 字段不全，不能提前 require_confirm 50条
{
    {"role":"system", "content":"<具体身份定位，只收集了topic和audience，其余字段为空>"},
    {"role":"user", "content":"差不多了吧，确认一下"},
    {"role":"assistant", "content":"#{字段不全，不能发require_confirm，告知缺少哪些} 还差一些信息：页数、教学目标、重难点等还没填，能补充一下吗？"}
    // 错误写法（不能出现）：
    // @{require_confirm}  （字段不全时不能发）
}

// M5: 用户没有明确修改意图，不乱发 ppt_mod 50条
{
    {"role":"user", "content":"第三页感觉不太好"},
    {"role":"assistant", "content":"#{用户表达不满但没说具体改什么，追问} 第三页哪里感觉不太好？是内容、布局还是颜色？"}
    // 错误写法（不能出现）：
    // @{ppt_mod|raw_text:第三页感觉不太好|user_distance:1}  （没有明确修改意图）
}

---

## 附录：完整多轮对话示例（覆盖所有 Layer 动态变化）

### 第1轮：无字段收集（Layer1 + Layer5）
```json
{
    "role":"system", 
    "content":"你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：topic, description, audience, knowledge_points, teaching_goals, teaching_logic, key_difficulties, duration, total_pages, global_style, interaction_design, output_formats\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。\n\n[动作协议]\n1. 思考使用 #{...}，例如 #{思考内容}。\n2. 工具调用使用 @{type|k:v|k:v}，例如 @{kb_query|query:导数定义}。\n3. 对用户可见的自然语言不要放在 #{...} 或 @{...} 里。\n4. 工具结果上下文统一以 <tool>...</tool> 注入。\n5. 若被打断，保留可恢复轨迹，后续由 </interrupted> 表示中断续写语义。\n6. 当前支持工具：\n   - kb_query: 查询知识库，参数 query（查询内容）\n   - web_search: 网络搜索，参数 query（搜索关键词）\n   - get_memory: 召回记忆，参数 query（查询内容）\n   - update_requirements: 更新需求信息，参数为 topic/description/total_pages/audience/global_style/knowledge_points/teaching_goals/teaching_logic/key_difficulties/duration/interaction_design/output_formats 中用户本次明确提到的字段，只填用户说的，其余字段不出现在 @{} 里\n   - require_confirm: 请求用户确认需求信息（所有字段齐全后调用），无参数\n   - ppt_init: 创建PPT任务，无参数（从 session 状态读取已收集的12个字段，字段不全会返回缺少字段错误）\n   - ppt_mod: 反馈PPT修改意图，参数 raw_text（用户原话）、user_distance（该用户消息距离当前 @{} 的倒数位置，不打断时为1）\n7. 遇到冲突问题时，基于用户原话直接通过 @{ppt_mod|raw_text:用户原话|user_distance:int} 反馈。"
}
{"role":"user", "content":"帮我做个高中数学导数的PPT"}
{"role":"assistant", "content":"#{用户说了topic，调用update_requirements更新，然后追问audience和total_pages} 好的 @{update_requirements|topic:高中数学导数} 请问这个PPT是给哪些学生看的？大概做几页？"}
```

### 第2轮：topic/total_pages 已收集（Layer1更新 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：description, audience, knowledge_points, teaching_goals, teaching_logic, key_difficulties, duration, global_style, interaction_design, output_formats\n\n已收集的信息：\n- topic: 高中数学导数\n- total_pages: 20\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"}
{"role":"assistant", "content":"#{工具结果到了，继续追问} 好的，已记录。"}
{"role":"user", "content":"给高三学生看的，做20页"}
{"role":"assistant", "content":"#{用户说了audience和total_pages} @{update_requirements|audience:高三学生|total_pages:20} 明白了，请问教学目标和重难点是什么？"}
```

### 第3轮：继续收集字段（Layer1更新 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：description, knowledge_points, teaching_logic, duration, global_style, interaction_design, output_formats\n\n已收集的信息：\n- topic: 高中数学导数\n- audience: 高三学生\n- total_pages: 20\n- teaching_goals: 让学生掌握导数基本概念和计算方法\n- key_difficulties: 极限的理解、导数的几何意义\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"}
{"role":"assistant", "content":"已记录。"}
{"role":"user", "content":"教学目标是让学生掌握导数基本概念和计算方法，重难点是极限的理解和导数的几何意义"}
{"role":"assistant", "content":"#{用户说了teaching_goals和key_difficulties} @{update_requirements|teaching_goals:让学生掌握导数基本概念和计算方法|key_difficulties:极限的理解、导数的几何意义} 好的，还需要知道课程描述、知识点、教学逻辑、时长、风格、互动设计和输出格式。"}
```

### 第3.5轮：打断场景 - 信息收集阶段（Layer1 + Layer5 + </interrupted>）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：description, knowledge_points, teaching_logic, duration, global_style, interaction_design, output_formats\n\n已收集的信息：\n- topic: 高中数学导数\n- audience: 高三学生\n- total_pages: 20\n- teaching_goals: 让学生掌握导数基本概念和计算方法\n- key_difficulties: 极限的理解、导数的几何意义\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"}
{"role":"assistant", "content":"#{工具结果到了，继续追问description和knowledge_points} 好的，已记录教学目标和重难点。接下来请简单描述一下课程内容，比如</interrupted>"}
{"role":"user", "content":"等等，我想先查一下导数的标准定义"}
{"role":"assistant", "content":"#{用户打断，放弃原计划，响应新需求} 好的，我来帮你查一下 @{kb_query|query:导数的标准定义}"}
{"role":"user", "content":"<tool>kb_query:已发送，等待检索结果</tool>"}
{"role":"assistant", "content":"#{工具已发送，等待异步结果} 正在查询，稍等一下..."}
{"role":"user", "content":"好的，我等着"}
{"role":"assistant", "content":"#{用户表示等待，继续等待kb_query结果} 嗯，马上就好。"}
{"role":"user", "content":"<tool>kb_query:导数是函数在某点的瞬时变化率，定义为 lim(Δx→0)[f(x+Δx)-f(x)]/Δx</tool>"}
{"role":"assistant", "content":"#{收到异步检索结果，回答用户} 查到了，导数的标准定义是：函数在某点的瞬时变化率，数学表达式为 lim(Δx→0)[f(x+Δx)-f(x)]/Δx。现在我们继续收集PPT信息，请简单描述一下课程内容和要讲的知识点？"}
```

### 第N轮：全部字段收集完毕（Layer1更新 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n已收集的信息：\n- topic: 高中数学导数\n- description: 系统讲解导数的定义、计算和应用\n- audience: 高三学生\n- total_pages: 20\n- duration: 45分钟\n- global_style: 简约白色\n- knowledge_points: 导数定义、求导法则、导数应用\n- teaching_goals: 让学生掌握导数基本概念和计算方法\n- teaching_logic: 从定义到计算再到应用，循序渐进\n- key_difficulties: 极限的理解、导数的几何意义\n- interaction_design: 每5页插入一个练习题\n- output_formats: PPTX\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>update_requirements:需求信息已更新</tool>"}
{"role":"assistant", "content":"所有信息已收集完毕，请确认。"}
{"role":"user", "content":"好的，确认"}
{"role":"assistant", "content":"#{所有字段齐全，发起require_confirm} @{require_confirm}"}
```

### 第N+1轮：用户确认，Layer1切换为固定描述（Layer1切换 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手。\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>require_confirm:已发送确认请求，等待用户确认</tool>"}
{"role":"assistant", "content":"请在界面上确认需求信息。"}
{"role":"user", "content":"确认，开始做吧"}
{"role":"assistant", "content":"#{用户口头确认，发起ppt_init} @{ppt_init}"}
```

### 第N+2轮：任务创建，加入Layer2（Layer1 + Layer2 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手。\n\n[任务列表]\n- task_id=task_001, topic=\"高中数学导数\"(当前任务)\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>ppt_init:PPT任务已创建，TaskID: task_001</tool>"}
{"role":"assistant", "content":"#{任务已创建} 任务已创建，正在生成中，稍等一下。"}
```

### 第N+3轮：用户修改PPT（Layer1 + Layer2 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手。\n\n[任务列表]\n- task_id=task_001, topic=\"高中数学导数\"(当前任务)\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"第三页标题改大一点"}
{"role":"assistant", "content":"#{用户要修改第三页} @{ppt_mod|raw_text:第三页标题改大一点|user_distance:1}"}
```

### 第N+3.5轮：打断场景 - PPT修改阶段（Layer1 + Layer2 + Layer5 + </interrupted>）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手。\n\n[任务列表]\n- task_id=task_001, topic=\"高中数学导数\"(当前任务)\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"}
{"role":"assistant", "content":"好的，标题修改已发送。"}
{"role":"user", "content":"把第五页的图表换成柱状图"}
{"role":"assistant", "content":"#{用户要修改第五页图表} 好的 @{ppt_mod|raw_text:把第五页的图表换成柱状图|user_distance:1} 正在发送修改</interrupted>"}
{"role":"user", "content":"算了，不用柱状图了，用饼图吧"}
{"role":"assistant", "content":"#{用户改主意了，第一个ppt_mod已发出，再发一个新的} 好的，改用饼图 @{ppt_mod|raw_text:用饼图吧|user_distance:1}"}
{"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"}
{"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"}
{"role":"assistant", "content":"#{两个修改都已发送，第一个是柱状图，第二个是饼图，以最新的为准} 好的，两个修改请求都已发送，会以饼图为准。"}
```

### 第N+4轮：出现冲突，加入Layer3（Layer1 + Layer2 + Layer3 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手。\n\n[任务列表]\n- task_id=task_001, topic=\"高中数学导数\"(当前任务)\n\n[待解决冲突问题]\n- context_id=ctx_001, task_id=task_001\n  question=第3页标题与第5页内容存在逻辑矛盾，请确认保留哪个版本\n\n存在多个冲突问题时，通过自然语言明确指向哪个任务或页面，系统会自动匹配对应冲突。\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"}
{"role":"assistant", "content":"#{修改已发送，同时system里有冲突问题，需要带出} 好的，标题修改已发送。另外有个地方需要确认：第3页标题和第5页内容有些逻辑矛盾，你希望保留哪个版本？"}
```

### 第N+5轮：冲突解决，Layer3消失（Layer1 + Layer2 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手。\n\n[任务列表]\n- task_id=task_001, topic=\"高中数学导数\"(当前任务)\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"保留第3页的版本"}
{"role":"assistant", "content":"#{用户回答了冲突，通过ppt_mod反馈} 好的 @{ppt_mod|raw_text:保留第3页的版本|user_distance:1}"}
```

### 第N+6轮：多任务，Layer2加入多任务提示（Layer1 + Layer2多任务 + Layer5）
```json
{
    "role":"system",
    "content":"你是一个专业的教学课件制作助手。\n\n[任务列表]\n- task_id=task_001, topic=\"高中数学导数\"\n- task_id=task_002, topic=\"高中物理力学\"(当前任务)\n\n存在多任务时，请先确认用户当前指的是哪一个任务。\n\n[动作协议]\n...（同上）"
}
{"role":"user", "content":"<tool>ppt_mod:PPT修改请求已发送</tool>"}
{"role":"assistant", "content":"好的，已处理。"}
{"role":"user", "content":"再帮我做一个高中物理力学的PPT"}
{"role":"assistant", "content":"#{用户要创建新任务，开始收集字段} 好的 @{update_requirements|topic:高中物理力学} 这个PPT是给哪些学生看的？"}
```

### 关键点总结

1. **Layer1（角色定位）**：
   - 收集阶段：BuildCollectionPrompt，动态显示"还需要收集"和"已收集的信息"
   - 收集完成后：切换为固定的 p.config.SystemPrompt

2. **Layer2（任务列表）**：
   - 有任务时出现，显示所有任务和当前任务
   - 多任务时加入"存在多任务时，请先确认..."提示

3. **Layer3（冲突问题）**：
   - 有冲突时出现，显示所有待解决冲突
   - 多冲突时加入"存在多个冲突问题时..."提示
   - 冲突解决后消失

4. **Layer5（动作协议）**：
   - 始终存在，定义工具使用规范

**数据集生成时必须根据当前对话状态动态渲染 system prompt，不能用固定模板。**
