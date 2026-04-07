核心思想就是，先1s 1s流式asr,把结果结合history，加一个<user>[obs]</user>预热kv-cache,然后<user>[obs][obs]</user>，一直到vad_end且2pass的结果出来的时候，ctx.cancel()掉所有的正在推理的goroutine,然后正式推理，推理的每个周期其实是think->output->[]action或者think->[]action->output,think是#{xxx},output是xxxxx正常输出，[]action是@{xxx|xxx}@{xxxx|xxxx}这样，然后output或[]action可以为空，但是如果两者同时为空，那么#{}后一定不能再有东西了，否则两个#{}#{}挨在一起是有问题的。如果被打  
断（用户又开始vad_start),那么如果这时候是@{xxxx|xxx}是最新输出（通过检测是否有@但是后面没有}判断），必须等待}输出了再cancel()掉然后上下文加</interrupted>,因为是流式输出，每次等1s检查下就行，如果下次检查虽然上次动作闭合了但是又有新的@没闭合，继续等，最多等3s(检查三次），如果第三次发现还没闭合成功，硬性cancel,然后把@包括其后面没闭合的从上下文里删去，然后用户vad_end后<user>xxxxx</user>以此类推；如果不是这类情况都可以直接打断加</interrupted>.然后流式的时候一旦每次解析出一个@{xxx|xxx}立马执行，所有的动作都是异步的，等待后续其他模块post给结果就好，给的结果有放context queue的，有放high priority listener里的，如果是context queue那就是一个工具的结果是<tool>xxxx</tool>即可,如果是high priority listener那就先等idle输出，输出总是被打断，超过3次就在下次有机会填充<tool>xxx</tool>的时候填充下。所以用go的表达方式，就是若type Tool = <tool>xxx</tool>,type User = <user>xxx</user>,type Action = @{xx|xxx},type Think=#{xxxxx},type Output = xxxx,type Model = Think+Output+[]Action / Think+[]Action+Output / Think+[]Action / Think+Output,type AI = Optional[ []Model ]+Optional[ Think ]+Optional[ </interrupted> ]的情况下，那么[]Tool随时可能穿插在User整体和AI整体之间的，通过这种方式可以管理上下文了，这下清楚了：ALL CONTEXT = User , AI , []Tool 的三人转。每次用户<user>xxx</user>的vad_start记录时间戳和PPT快照，一遍后续用（比如ppt_mod需要用户说话时候的时间戳和快照状态）。

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