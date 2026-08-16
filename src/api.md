
## module 0 : voice turn api

the frontend handles vad_start and vad_end locally. no audio chunks are streamed before vad_end.the backend runs local asr with qwen3-asr-0.6b(https://www.modelscope.cn/models/Qwen/Qwen3-ASR-0.6B).we do not have a tts model;the frontend feeds the `tts` chunks to its own tts engine.

### 0.1 Post api/v1/voice/vad_start

this endpoint is kept as a lightweight hook for the frontend to sync the assistant turn state. there is no interrupt-check llm and no fast asr judgment: the frontend vad already filters out noise (min speech duration), so the backend always treats detected speech as an interrupt.

request body:

```json
{}
```

empty json placeholder. the frontend does not upload any audio here, so no extra data transfer latency is added.

backend processing:

return immediately without any model inference.

response body:

```json
{
    "code": 200,
    "message": "success",
    "data": {
        "interrupt": true
    }
}
```

frontend behavior after response (always `interrupt: true`):

if no assistant turn is active, this is just a normal new turn: record the audio and go to 0.2.

if an assistant turn is active, the interrupt falls into one of three scenarios, decided by the **stream state** (whether the opening `<` of the first `<tool_call>` has been emitted) and the **tts state** (whether tts has finished playing):

- **scenario 1 (abort)**: the backend stream has not yet emitted the opening `<` of the first `<tool_call>`. the frontend stops tts playback immediately and aborts the stream; no tool call is generated. the assistant message kept in history is truncated at the **tts-played position** (what the user actually heard), not at the inference position — the tokens between the tts-played position and the inference position are discarded (a few tokens wasted, but few). the next user saying words message gets the `</interrupted>` prefix.
- **scenario 2 (continue)**: the opening `<` of the first `<tool_call>` has already been emitted but tts has not finished playing. the frontend lets tts finish playing the remaining spoken text, and lets the backend goroutine continue until the **full tool call sequence** is complete and the tool responses are executed. a single `<tool_call>...</tool_call>` may be fully closed, but if the goroutine is still running, there may be more `<tool_call>...</tool_call>` tags following. the tool calls themselves are silent, so the only sound the user hears is the remaining spoken text playing to completion. the next user message gets **no** `</interrupted>` prefix (see the history order note in 0.2).
- **scenario 3 (continue)**: the opening `<` of the first `<tool_call>` has already been emitted and tts has already finished playing. the tts line is done long ago; everything else is the same as scenario 2.

in scenarios 2/3 the next inference starts only when **three lines all complete**:

1. the tts line: tts playback of the previous turn finishes (in scenario 3 this line is already complete).
2. the tool call line: the previous turn's tool call sequence finishes executing and every tool response result is obtained. the results are **not** consumed by a report pass (no post-tool-call tts, unlike the normal flow in 0.2) — they stay pending and go into the next turn's assembly below.
3. the asr line: the audio from the interrupt start to the user's speech end (ring buffer prefix prepended) is transcribed. the user may immediately say another sentence while the other two lines are still running — the user may interrupt multiple times. each speech segment becomes its own user message in the final assembly (see below).

**line state (backtracking):** the waiting process has a single state: not complete / complete. while waiting, **every** new interrupt backtracks the **asr line** to its not-complete state and the audio is recollected (the asr line extends; the tts line and the tool call line are only waited on — they belong to the finished previous turn and never backtrack). the process becomes complete only when the **last** speech segment has ended and the other two lines are done; only then does the backend assemble `tool responses` + `user saying words` + `status bar` and start the next llm inference. in that assembly, the saying words part may be **multiple user messages** (one per speech segment, in speaking order), same as the tool response messages; the status bar is always exactly **one** user message and always last (see 0.2).

even after all three lines are done, the process is still "waiting" until the llm actually starts the next inference: an interrupt before that moment backtracks the **asr line** to not-complete again (the tts line and the tool call line stay complete) — the same waiting process, no recursion. only an interrupt **after** the llm really started the next inference is an interrupt of the new turn, which re-enters scenarios 1/2/3 recursively.

- in scenario 1, the assistant message that goes into the conversation history contains only the truncated spoken text. in scenarios 2/3 it contains the complete spoken text plus the complete tool call sequence. in **all** scenarios the assembled context also ends with the status bar user message (`<queue_status>empty|not empty</queue_status>`, the always-last user message, see 0.2).

### 0.2 Post api/v1/voice/vad_end

call this right after vad_end.

request body:

```json
{
    "audio": "base64-encoded audio: the ring buffer prefix (audio from just before the interrupt) + the audio from vad_start to vad_end",
    "format": "pcm",
    "needs_interrupted_prefix": true,
    "interrupted_assistant_text": "new version ppt alre"
}
```

- `needs_interrupted_prefix`: decided by the frontend, following the three scenarios in 0.1. `true` only in scenario 1 (the stream was aborted before the first `<tool_call>` was emitted); in scenarios 2/3 it is `false` even if tts was still playing when the interrupt happened, because the tool call sequence completed and the context shows no `</interrupted>`.
- `interrupted_assistant_text`: the truncated assistant text that had already been spoken before the interrupt. the backend appends it to the conversation history before starting the new inference so the llm context stays consistent. empty string when there is no truncated text to sync.

backend processing:

1. run local asr(qwen3-asr-0.6b) on each speech segment as its vad_end arrives (ring buffer prefix + that segment's audio from its vad_start to its vad_end). with repeated interrupts, each segment is transcribed separately; the transcripts accumulate while the turn waits.
2. scenario 1: the previous stream was already aborted, nothing is running. send the transcript directly to the voice agent llm and stream the response back to the frontend token by token. no interrupt-check llm.
3. scenarios 2/3: the previous turn's tool call sequence must first finish executing and its tool responses be obtained (the tool call line). while it runs, the transcript of every speech segment that arrives is held (the asr line). when the tool call line is done, assemble the multiple user messages in the fixed order (tool responses → all held saying words segments → status bar, see the history order note below) and start the next llm inference. a vad_end that arrives before the inference actually starts backtracks the asr line (the waiting process in 0.1); one that arrives after it recurses into the three scenarios.

streamed response format (server-sent events):

the voice agent llm follows the qwen3 chat template. the backend provides the tools in the system prompt as qwen3 `<tools>...</tools>` xml, and the model emits tool calls as `<tool_call>{"name": "...", "arguments": {...}}</tool_call>`; tool results are fed back as `<tool_response>...</tool_response>`.

| chunk type | example | meaning |
|------------|---------|---------|
| user_transcript | `{"type": "user_transcript", "text": "<\|im_start\|>user\n<tool_response>\nall fields are updated\n</tool_response>\n<\|im_end\|>\n<\|im_start\|>user\nxxxxx\n<\|im_end\|>\n<\|im_start\|>user\nyyyyy\n<\|im_end\|>\n<\|im_start\|>user\n<queue_status>not empty</queue_status>\n<\|im_end\|>"}` | the fully formatted **user messages** for this turn, emitted first so the frontend can append them to history as **multiple** user messages. the example shows the multi-interrupt case: `xxxxx` and `yyyyy` are two separate speech segments the user said while waiting (repeated interrupts), each one its own `user` message. |
| tts token | `{"type": "tts", "text": "好的"}` | a piece of tts text. the frontend feeds these tokens to its own tts engine. |
| tool call | `{"type": "tool_call", "payload": "{\"name\": \"update_requirements\", \"arguments\": {\"topic\": \"math\"}}"}` | a complete `<tool_call>` json object extracted from the llm stream. |
| tool result | `{"type": "tool_result", "text": "all fields are updated"}` | the synchronous result of the tool call just emitted. fed back to the model as a `<tool_response>` block (see the history order note below). |
| turn end | `{"type": "turn_end"}` | signals the end of this assistant turn |

note: the backend executes every tool call synchronously. the stream order is `user_transcript` → `tts` (spoken text) → `tool_call` → `tool_result` → `tool_call` → `tool_result` → ... → **[optional] `tts`** → `turn_end`. **after all tool calls are emitted, the assistant may output additional tts text to report the tool results, but no further tool calls are allowed after that.** the report pass (the post-tool-call tts) only happens in the normal flow: in the interrupted scenarios 2/3 of 0.1, the tool results stay pending and go into the next turn's assembly instead.

history order (status bar technology):

when assembling the conversation for the next inference, the assistant message that goes into the history contains the text that had **already been spoken** (a complete sentence or a truncated half-sentence) plus the complete tool call sequence only if the stream had already entered the tool call phase. after it, **multiple user messages are assembled in this fixed order: `tool response message(s)` → `user's saying words message` → `status bar message`**. following the qwen3 chat template, each is rendered with `<|im_start|>user` / `<|im_end|>` markers:

```text
<|im_start|>user
<tool_response>
all fields are updated
</tool_response>
<|im_end|>
<|im_start|>user
xxxxx
<|im_end|>
<|im_start|>user
<queue_status>not empty</queue_status>
<|im_end|>
```

- the tool response message(s) only exist when there are pending tool results (scenarios 2/3). in the qwen3 chat template, tool results are rendered inside a `user` role message (consecutive `tool` messages are auto-batched by the template into one `user` message), so each batch of pending `<tool_response>` blocks forms its own `user` message placed first.
- `xxxxx` is the user's saying words of this turn, plain text with no wrapper tag, in its own `user` message. if the user said several sentences while waiting (repeated interrupts), each speech segment becomes its **own** `user` message, in speaking order — the saying words part may be multiple messages.
- `<queue_status>empty|not empty</queue_status>` is the status bar: it records in real time whether the ppt message queue is empty. it is a **separate `user` message and always the last message** in the user prompt.
- `</interrupted>` only appears in scenario 1 (the previous stream was aborted before the tool call phase); it is the very first thing in the saying words message. in scenarios 2/3 the context shows no `</interrupted>` at all — the model sees its own completed tool call sequence and the pending tool results, so nothing was lost and nothing needs to be deferred.
- if there are no pending tool results, the assembled messages start directly with the saying words message.
- **if there is post-tool-call tts, it becomes a second, independent `assistant` message placed after the tool result message(s) and before the final user messages—not merged into the first assistant message.** in that case the tool results were already consumed by that report, so the assembled user messages only contain the saying words message + the status bar message.

### 0.3 Post api/v1/voice/text_input

text-only debug/testing path. skips asr and feeds the text directly into the voice agent stream.

request body:

```json
{
    "text": "string"
}
```

backend processing:

same as 0.2 step 2, with `needs_interrupted_prefix: false` and empty `interrupted_assistant_text`. the streamed response format is identical to 0.2.

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

