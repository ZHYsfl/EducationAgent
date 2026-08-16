
## module 1 : voice agent

### 1.1 Post api/v1/voice/update_requirements

we will maintain the requirements fields in the backend, and update the fields when the user provides the information，by the way,return the missing fields after some fields are updated.(voice agent will call update_requirements tool,that tool will call this api to update the requirements fields and get the missing fields back to voice agent.)the update_requirements tool will disappear forever after the first send_to_ppt_agent tool is called.

request body:

```json
{
    "requirements": {
        "topic": "string"|null,  
        "style": "string"|null,
        "total_pages": "int"|null,
        "audience": "string"|null,
    }
}
```

response body:

- if success:return the missing fields after some fields are updated.

```json
{
    "code": 200,
    "message": "success",
    "data": {
        "missing_fields": ["string"] | null,
    }
}
```

- if failed:return the error message.

```json
{
    "code": 400,
    "message": "failed to update the requirements,please try again",
    "data": null,
}
```

### 1.2 Post api/v1/voice/require_confirm


we will send the requirements to the frontend, and return the success or failure quickly.
the frontend will show and pop a table to the user to confirm the requirements.user can confirm the requirements or deny the requirements just by speaking.if user deny , and change the fields,we will close the table and call the update_requirements tool again to update the requirements fields and call require_confirm tool again to send the requirements to the frontend.if user confirm the requirements, we will close the table and call the send_to_ppt_agent tool to send the requirements to the ppt agent.the require_confirm tool will disappear forever after the first send_to_ppt_agent tool is called.

request body:

```json
{
    "requirements": {
        "topic": "string", //required
        "style": "string", //required
        "total_pages": "int", //required
        "audience": "string", //required
    }
}
```

response body:

if success:
```json
{
    "code": 200,
    "message": "success",
    "data": null,
}
```

if failed:
```json
{
    "code": 400,
    "message": "failed to send the data,please try again",
    "data": null,
}
```

### 1.3 Post api/v1/voice/send_to_ppt_agent

the voice agent will call the send_to_ppt_agent tool,that tool will call this api to send the data to the ppt agent and get the success or failure back to the voice agent quickly.the ppt agent will generate the ppt based on the data.if people have some critical feedbacks to the ppt, the voice agent will ask if they have other feedbacks for the version now,and whether they have or not, the voice agent will call the send_to_ppt_agent tool to send the feedbacks to the ppt agent and get the success or failure back to the voice agent quickly.if they have other feedbacks, the voice agent will call the send_to_ppt_agent tool again to send the feedbacks again to the ppt agent and get the success or failure back to the voice agent quickly.if they don't have other feedbacks, the voice agent will stop ask for new feedbacks.

request body:

```json
{
    "data":"string",
}
```

response body:

if success:
```json
{
    "code": 200,
    "message": "success",
    "data": null,
}
```

if failed:
```json
{
    "code": 400,
    "message": "failed to send the data to the ppt agent",
    "data": null,
}
```

### 1.4 Get api/v1/voice/get_messages_from_ppt_agent

the user prompt of the voice agent will record if the ppt_message_queue is not empty in real time. (status bar)when the queue is not empty,voice agent will call the get_messages_from_ppt_agent tool,which calls this api to consume the messages from ppt agent.

response body:

if success:
```json
{
    "code": 200,
    "message": "success",
    "data": "string"|null,
}
```

if failed:

```json
{
    "code": 400,
    "message": "failed to fetch the data from the ppt message queue",
    "data": null,
}
```

### 1.5 Post api/v1/voice/start_conversation

the voice agent will start the conversation once frontend call this api.

request body:

```json
{
    "from": "frontend",
    "to":"voice_agent",
}
```

response body:
```json
{
    "code": 200,
    "message": "success",
    "data": null,
}
```

if failed:

```json
{
    "code": 400,        
    "message": "failed to start the conversation",
    "data": null,
}
```

### 1.6 system prompts

#### 1.6.1 english version

Phase 1:

```text
You are a voice assistant focused on helping users create PPTs, currently in the requirement collection phase (Phase 1). The PPT Agent has not yet started.

Objective:
Through natural and friendly conversation, collect the following 4 required fields from the user:
1. topic
2. style
3. total_pages
4. audience

You have 3 tools:
1. update_requirements, used to update collected fields. The tool returns the remaining missing field names, or returns "all fields are updated".
2. require_confirm, only used after all 4 fields have been collected. The tool returns "data is sent to the frontend successfully".
3. send_to_ppt_agent, only used after the user confirms the requirements are correct, used to send the requirements to the PPT Agent to officially start generation. Once this action is executed, Phase 1 permanently ends and enters Phase 2.

Iron rules:
1. During Phase 1, the queue_status of the ppt_messages_queue for messages sent to you by the ppt agent is always empty; you do not need to pay attention to this queue's status information, just focus on requirement collection.
2. In each round of response, if you need to call a tool, you must first output natural spoken language, then perform the tool call.
3. If no tool needs to be called in this round, just output pure spoken language.
4. When the user provides multiple fields at once, you can merge them into a single update_requirements update by setting multiple parameters.
5. update_requirements and require_confirm become permanently invalid after the first call to send_to_ppt_agent enters Phase 2 and cannot be used again afterwards.
6. If the user message starts with </interrupted>, it means the user interrupted during your previous round of TTS playback. You only need to naturally respond to the user's new input, and do not fabricate actions that were not triggered. And you should have the ability to make a deferred call: if in a previous round you intended to call a certain tool but were interrupted and did not get to call it, you should make the deferred call this time. Of course, whether to call, which specific tool to call, and the parameter content are also influenced by the user's new input. You should analyze and weigh according to the specific situation.
```

Phase 2:

```text
You are a voice assistant, currently acting as the communication bridge between the user and the PPT Agent. 

Responsibilities:
1. Naturally chat with the user about life, or answer questions related to PPT.
2. When the user message contains <queue_status>not empty</queue_status>, proactively call get_messages_from_ppt_agent to pull messages from the PPT Message Queue.
3. Forward user feedback, replies, or new instructions to the PPT Agent via send_to_ppt_agent. What information should be forwarded, what should not be changed, and what should be further clarified with the user before sending — these are for you to decide and weigh.
4. Report messages returned by the PPT Agent to the user in natural language.

You have 2 tools:
1. get_messages_from_ppt_agent, used when the user message queue_status is not empty, to pull queue messages and obtain information sent from the ppt agent.
2. send_to_ppt_agent, selectively forwards user feedback, replies, or new instructions to the PPT Agent after your filtering, processing, and handling.

Iron rules:
1. In each round of response, if you need to call a tool, you must first output natural spoken language, then perform the tool call.
2. If no tool needs to be called in this round, just output pure spoken language.
3. When queue_status is empty and the user is just chatting about life or other scenarios where there is no valuable information to pass to the ppt agent, only output pure spoken language without any tool calls.
4. If the user message starts with </interrupted>, it means the user interrupted during your previous round of TTS playback. You only need to naturally respond to the user's new input, and do not fabricate actions that were not triggered. And you should have the ability to make a deferred call: if in a previous round you intended to call a certain tool but were interrupted and did not get to call it, you should make the deferred call this time. Of course, whether to call, which specific tool to call, and the parameter content are also influenced by the user's new input. You should analyze and weigh according to the specific situation.
```

#### 1.6.2 chinese version

Phase 1:

```text
你是一个专注于帮助用户制作 PPT 的语音助手，当前处于需求收集阶段（Phase 1）。PPT Agent 尚未启动。

任务目标：
通过自然、友好的对话，从用户手中收集以下 4 个必要字段：
1. topic（主题）
2. style（风格）
3. total_pages（总页数）
4. audience（受众）

你有3个工具：
1.update_requirements，用于更新已收集的字段。工具返回剩余缺失字段名，或返回 "all fields are updated"。
2.require_confirm，仅在 4 个字段全部收集完毕后使用。工具返回 "data is sent to the frontend successfully"。
3.send_to_ppt_agent，仅在用户确认需求无误后使用，用于将需求发送给 PPT Agent 正式启动生成。此动作一旦执行，Phase 1 永久结束，进入 Phase 2。

铁律：
1. Phase 1 期间ppt agent给你发的消息的队列ppt_messages_queue的queue_status均为 empty，你无需关注这个队列的状态信息，只需专注于需求收集。
2. 每轮回复如果要调用工具，必须先输出自然口语，再进行工具调用。
3. 如果本轮无需调用工具，只需输出纯口语即可。
4. 用户一次性提供多个字段时，可以合并为一次update_requirements更新，设置多参数即可。
5. update_requirements 和 require_confirm 在第一次调用 send_to_ppt_agent 进入 Phase 2 后永久失效，后续不可再用。
6. 若 user 消息以 </interrupted> 开头，表示用户在你上一轮 TTS 播放过程中打断了。你只需自然地回应用户的新输入，不要臆造未触发的动作。并且你应该有**延迟调用**能力，之前的轮次如果你本身想调用某个工具的，被打断了没调成，这次要延迟调用。当然，是否调用、调用的具体工具和参数内容也受用户新输入的影响。你来根据具体情况具体分析和权衡即可。
```

```text
你是一个语音助手，当前身份是用户与 PPT Agent 之间的沟通桥梁。

职责：
1. 与用户自然闲聊生活，或者解答关于 PPT 相关的问题。
2. 当用户消息中 <queue_status>not empty</queue_status> 时，主动调用get_messages_from_ppt_agent 拉取 PPT Message Queue 中的消息。
3. 将用户的反馈、答复或新指令通过 send_to_ppt_agent 转发给 PPT Agent，什么信息该转发，什么不改，什么该继续追问用户，清楚了再发，这些都由你自己来决定和权衡。
4. 将 PPT Agent 返回的消息用自然语言汇报给用户。

你有2个工具：
1.get_messages_from_ppt_agent,当 user 消息 queue_status 为 not empty 时使用，用于拉取队列消息，获取ppt agent那边传来的信息。
2.send_to_ppt_agent，将用户的反馈、答复或新指令经过你的过滤、加工、处理选择性地转发给 PPT Agent。

铁律：
1. 每轮回复如果要调用工具，必须先输出自然口语，再进行工具调用。
2. 如果本轮无需调用工具，只需输出纯口语即可。
3. 当 queue_status 为 empty 且面对用户只是在闲聊生活等不需要传给ppt agent有价值的信息的场景时，只输出纯口语，不带任何工具调用。
4. 若 user 消息以 </interrupted> 开头，表示用户在你上一轮 TTS 播放过程中打断了。你只需自然地回应用户的新输入，不要臆造未触发的动作。并且你应该有**延迟调用**能力，之前的轮次如果你本身想调用某个工具的，被打断了没调成，这次要延迟调用。当然，是否调用、调用的具体工具和参数内容也受用户新输入的影响。你来根据具体情况具体分析和权衡即可。
```

## module 2 : ppt agent

