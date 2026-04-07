核心思想就是，先1s 1s流式asr,把结果结合history，预热kv-cache（thinkLoop纯warmup，所有token丢弃），一直到vad_end且2pass的结果出来的时候，ctx.cancel()掉所有的正在推理的goroutine,然后正式推理，推理的每个周期其实是think->output->[]action或者think->[]action->output,think是#{xxx},output是xxxxx正常输出，[]action是@{xxx|xxx}@{xxxx|xxxx}这样，然后output或[]action可以为空，但是如果两者同时为空，那么#{}后一定不能再有东西了，否则两个#{}#{}挨在一起是有问题的。如果被打  
断（用户又开始vad_start),那么如果这时候是@{xxxx|xxx}是最新输出（通过检测是否有@但是后面没有}判断），必须等待}输出了再cancel()掉然后上下文加</interrupted>,因为是流式输出，每次等1s检查下就行，如果下次检查虽然上次动作闭合了但是又有新的@没闭合，继续等，最多等3s(检查三次），如果第三次发现还没闭合成功，硬性cancel,然后把@包括其后面没闭合的从上下文里删去，然后用户vad_end后<user>xxxxx</user>以此类推；如果不是这类情况都可以直接打断加</interrupted>.然后流式的时候一旦每次解析出一个@{xxx|xxx}立马执行，所有的动作都是异步的，等待后续其他模块post给结果就好，给的结果统一放context queue，工具结果以<tool>xxxx</tool>形式通过flushToolResults()在每次LLM调用前写入对话历史（role: tool），不注入system prompt。所以用go的表达方式，就是若type Tool = <tool>xxx</tool>,type User = <user>xxx</user>,type Action = @{xx|xxx},type Think=#{xxxxx},type Output = xxxx,type Model = Think+Output+[]Action / Think+[]Action+Output / Think+[]Action / Think+Output,type AI = Optional[ []Model ]+Optional[ Think ]+Optional[ </interrupted> ]的情况下，那么[]Tool随时可能穿插在User整体和AI整体之间的，通过这种方式可以管理上下文了，这下清楚了：ALL CONTEXT = User , AI , []Tool 的三人转。每次用户<user>xxx</user>的vad_start记录时间戳和PPT快照，一遍后续用（比如ppt_mod需要用户说话时候的时间戳和快照状态）。

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

p.config.SystemPrompt/
BuildCollectionPrompt：
你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n还需要收集以下信息：%s\n\n（"topic", "description", "audience", "knowledge_points","teaching_goals","teaching_logic", "key_difficulties","duration", "total_pages","global_style", "interaction_design",output_formats"）已收集的信息：- 主题: %s\n- 描述: %s\n- 受众: %s\n- 页数: %d\n- 时长: %s\n- 风格: %s\n- 知识点: %s\n- 教学目标: %s\n- 教学逻辑: %s\n- 重难点: %s\n- 互动设计: %s\n- 输出格式: %s\n\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。

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
6. 当前支持工具: kb_query, web_search, update_requirements, require_confirm, ppt_init, ppt_mod, get_memory。
7. 遇到冲突问题时，基于用户原话直接通过 @{ppt_mod|raw_text:用户原话|user_distance:int} 反馈。




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

工具调用数据集(2500条):
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

// A1: 每个工具单轮调用100条数据集（1次快速成功）
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 自然语言输出 @{tool|k:v}"},
    {"role":"user", "content":"<tool>tool_type:结果内容</tool>"},
    {"role":"assistant", "content":"#{基于工具结果推理} 基于工具结果的回复"}
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

// B3: @{} 输出到一半超3s被硬截断 50条
// 不完整的 @{ 被删除，</interrupted> 追加在截断处
{
    {"role":"system", "content":"<具体身份定位>"},
    {"role":"user", "content":"用户说的话"},
    {"role":"assistant", "content":"#{思考} 让我查一下</interrupted>"},
    // 注：原始输出可能是 "让我查一下 @{kb_query|quer" 但不完整@{被删除
    {"role":"user", "content":"用户新指令"},
    {"role":"assistant", "content":"#{被打断，工具未发出，响应新指令} 好的..."}
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