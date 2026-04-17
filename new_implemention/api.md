api.md

## go-backend api

we follow the mvp rule to build things fast and iteratively.all the tools have context.Context as the first argument.the backend program is deployed in a docker sandbox.the sandbox has node.js,go,slidev installed.the voice agent llm will be finetined by us,and the ppt agent llm will directly use the sota llm api.

### module 0: voice turn api

the frontend handles vad_start and vad_end locally. no audio chunks are streamed before vad_end.

#### 0.1 Post `/api/v1/voice/vad_start`

when the frontend detects `vad_start`, it begins buffering microphone data into a local ring buffer. it waits until either `vad_end` fires or `1.5s` elapses, whichever comes first. only then does it send the captured audio segment to the backend for a fast interrupt check.

request body:
```json
{
    "audio": "base64-encoded audio from vad_start to min(vad_end, vad_start + 1.5s)",
    "format": "pcm"
}
```

backend processing:
1. run local asr on the short audio segment.
2. send the transcript to the interrupt-check llm to decide if this is a real interruption.

response body:
```json
{
    "code": 200,
    "message": "success",
    "data": {
        "interrupt": true | false
    }
}
```

frontend behavior after response:
- if `interrupt: true`:
  - if the assistant turn is active, the frontend immediately clears the tts queue and stops playback, and discards any buffered tokens.
  - if the backend stream has not yet emitted the opening `<` of the first `<action`, the stream is aborted and no action is generated.
  - if the opening `<` has already been emitted, the frontend must let the goroutine continue running until the **full action sequence** is complete. a single `<action>...</action>` may be fully closed, but if the goroutine is still running, there may be more `<action>...</action>` tags following. because actions are silent (not tts-played), the user will not experience "the machine is still talking".
  - the assistant message that goes into the conversation history contains the text that had **already been spoken** (it may be a complete sentence or a truncated half-sentence) plus the complete action sequence only if the stream had already entered the action phase.
  - when vad_end arrives, the frontend sends the full audio to `POST /api/v1/voice/vad_end`. the backend runs full asr and skips the interrupt-check llm because the fast check has already confirmed this is a real turn. however, if the action sequence from the previous turn is still running, the backend must wait for it to finish before starting the voice agent llm.
- if `interrupt: false`:
  - if the assistant turn is active, the frontend continues playing tts as if nothing happened. the conversation history is unchanged.
  - when vad_end arrives, the frontend still sends the full audio to `POST /api/v1/voice/vad_end`. the backend reuses the `interrupt: false` result and returns `{"ignored": true}` without calling the voice agent llm.

#### 0.2 Post `/api/v1/voice/vad_end`

call this right after vad_end.

request body:

when `vad_start` returned `interrupt: false`:
```json
{
    "audio": "base64-encoded audio from vad_start to vad_end",
    "format": "pcm"
}
```

when `vad_start` returned `interrupt: true`:
```json
{
    "audio": "base64-encoded audio from vad_start to vad_end",
    "format": "pcm",
    "needs_interrupted_prefix": true,
    "interrupted_assistant_text": "新版 PPT 已经"
}
```
- `needs_interrupted_prefix`: decided by the frontend. `true` only when the assistant was still playing TTS text to the user when the interrupt happened.
- `interrupted_assistant_text`: the truncated assistant text that had already been spoken before the interrupt. the backend appends it to the conversation history before starting the new inference so the LLM context stays consistent. empty string when there is no truncated text to sync.

backend processing:
1. run local asr on the full audio segment. this asr can start in parallel with the short asr from `vad_start` because the audio is just a prefix extension. the two asr jobs are cascaded (they may share the same session state) but the full asr does not need to wait for the fast check to finish.
2. look up the fast-check result stored for this turn:
   - if `vad_start` returned `interrupt: false`, return immediately:
     ```json
     {
         "code": 200,
         "message": "success",
         "data": {
             "ignored": true
         }
     }
     ```
     the frontend discards this turn and does nothing.
   - if `vad_start` returned `interrupt: true`, skip the interrupt-check llm and send the transcript directly to the finetuned voice agent llm. stream the response back to the frontend token by token.

streamed response format (server-sent events or websocket chunks):

| chunk type | example | meaning |
|------------|---------|---------|
| user_transcript | `{"type": "user_transcript", "text": "</interrupted>\\n<status>not empty</status>\\n<user>xxxxx</user>"}` | the fully formatted user message for this turn, emitted first so the frontend can append it to history. |
| tts token | `{"type": "tts", "text": "好的"}` | a piece of tts text. the frontend feeds these tokens to the tts engine |
| action | `{"type": "action", "payload": "update_requirements|topic:数学"}` | a parsed action extracted from the llm stream |
| tool | `{"type": "tool", "text": "all fields are updated"}` | the synchronous result of the action just emitted. inserted into the conversation history as an independent `tool` message after the assistant turn ends. |
| turn end | `{"type": "turn_end"}` | signals the end of this assistant turn |

note: the backend executes every action synchronously. the stream order is `user_transcript` → `tts` (spoken text) → `action` → `tool` → `action` → `tool` → ... → **[optional] `tts`** → `turn_end`. **after all actions are emitted, the assistant may output additional tts text to report the action results, but no further actions are allowed after that.** the tool result is a plain string (no xml wrapper). in history, it is stored as a separate `tool` role message following the `assistant` message that emitted the corresponding `<action>`. **if there is post-action tts, it becomes a second, independent `assistant` message placed after all `tool` messages—not merged into the first assistant message.**

### module 1: voice agent

#### 1.1 Post api/v1/update_requirements

request body:
```json
{
    "from":"frontend",
    "to":"voice_agent",
    "requirements": {
        "topic": "string"|null,  
        "style": "string"|null,
        "total_pages": "int"|null,
        "audience": "string"|null,
    }
}
```

response body:
if success:
return the missing fields after some fields are updated.
```json
{
    "code": 200,
    "message": "success",
    "data": {
        "missing_fields": ["string"] | null,
    }
}
```
if failed:
return the error message.
```json
{
    "code": 400,
    "message": "failed to update the requirements,please try again",
    "data": null,
}
```

we will maintain the requirements fields in the backend, and update the fields when the user provides the information，by the way,return the missing fields after some fields are updated.(voice agent will call update_requirements tool,that tool will call this api to update the requirements fields and get the missing fields back to voice agent.)

for instance:

```json
{
    "from":"frontend",
    "to":"voice_agent",
    "requirements": {
        "topic": "math",
        "style": "simple and elegant",
    }
}


```json
{
    "code": 200,
    "message": "success",
    "data": {
        "missing_fields": ["total_pages", "audience"]
    }
}
```

```json
{
    "from":"voice_agent",
    "to":"frontend",
    "requirements": {
        "total_pages": 15,
        "audience": "middle school students"
    }
}
```

```json
{
    "code": 200,
    "message": "success",
    "data": {
        "missing_fields": null
    }
}
```

the update_requirements tool function definition:
LLM:
```text
<action>update_requirements|topic:...|style:...|total_pages:...|audience:...</action>
```
go：
```go
func update_requirements(ctx context.Context, requirements map[string]any) (string, error) {
    // update the requirements fields in the backend
    // success: return a plain string telling the missing fields or completion
    return "we now still missing total_pages, audience", nil
    // or
    return "all fields are updated", nil
    // failure: return the error message directly
    return "failed to update the requirements,please try again", errors.New("failed to update the requirements,please try again") or ctx.Err()
}
```

tool result examples (plain string returned by the backend):
- success (still missing fields): `we now still missing total_pages, audience`
- success (complete): `all fields are updated`
- failure: `failed to update the requirements,please try again`

LLM -> parse the fields and their value,make the map[string]any,and call the update_requirements tool function->get the return value quickly -> LLM -> ask the user to provide the missing fields.
if all fields are updated, LLM will call the require_confirm tool to ask the user to confirm the requirements.

the update_requirements tool will disappear forever after the first send_to_ppt_agent tool is called.

---

#### 1.2 Post api/v1/require_confirm

request body:
```json
{
    "from": "voice_agent",
    "to": "frontend",
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
    "message": "failed to send the data to the frontend",
    "data": null,
}
```

we will send the requirements to the frontend, and return the success or failure quickly.
the frontend will show and pop a table to the user to confirm the requirements.user can confirm the requirements or deny the requirements just by speaking.if user deny , and change the fields,we will close the table and call the update_requirements tool again to update the requirements fields and call require_confirm tool again to send the requirements to the frontend.if user confirm the requirements, we will close the table and call the send_to_ppt_agent tool to send the requirements to the ppt agent.
Notice:this tool will return the success or failure quickly,and will not wait for the user to confirm the requirements.so the response data is just a message of if the data is sent to the frontend successfully.

the require_confirm tool function definition:
LLM:
```text
<action>require_confirm</action>
```
go:
```go
func require_confirm(ctx context.Context, requirements map[string]any) (string, error) {
    // require the user to confirm the requirements
    // success: plain string confirmation
    return "data is sent to the frontend successfully", nil
    // failure: plain string error
    return "failed to send the data to the frontend", errors.New("failed to send the data to the frontend") or ctx.Err()
}
```

tool result examples:
- success: `data is sent to the frontend successfully`
- failure: `failed to send the data to the frontend`

for instance:
```json
{
    "from": "voice_agent",
    "to": "frontend",
    "requirements": {
        "topic": "math",
        "style": "simple and elegant",
        "total_pages": 15,
        "audience": "middle school students"
    }
}
```

```json
{
    "code": 200,
    "message": "success",
    "data": null,
}
```

the require_confirm tool will disappear forever after the first send_to_ppt_agent tool is called.

---

#### 1.3 Post api/v1/send_to_ppt_agent

request body:
```json
{
    "from": "voice_agent",
    "to": "ppt_agent",
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

voice agent will send the data to the ppt agent, and return the success or failure quickly.
the ppt agent will generate the ppt based on the data.
the voice agent will call the send_to_ppt_agent tool,that tool will call this api to send the data to the ppt agent and get the success or failure back to the voice agent quickly.
Notice:this tool will return the success or failure quickly,and will not wait for the ppt agent to generate the ppt.so the response data is just a message of if the data is sent to the ppt agent successfully.


the send_to_ppt_agent tool function definition:
LLM:
```text
<action>send_to_ppt_agent|data:...</action>
```
go:
```go
func send_to_ppt_agent(ctx context.Context, data string) (string, error) {
    // send the data to the ppt agent
    // success: plain string confirmation
    return "data is sent to the ppt agent successfully", nil
    // failure: plain string error
    return "failed to send the data to the ppt agent", errors.New("failed to send the data to the ppt agent") or ctx.Err()
}
```

tool result examples:
- success: `data is sent to the ppt agent successfully`
- failure: `failed to send the data to the ppt agent`

for instance:
```json
{
    "from": "voice_agent",
    "to": "ppt_agent",
    "data": "user's requirements are: topic: math, style: simple and elegant, total_pages: 15, audience: middle school students",
}
```

```json
{
    "code": 200,
    "message": "success",
    "data": null,
}
```

if people have some critical feedbacks to the ppt, the voice agent will ask if they have other feedbacks for the version now,and whether they have or not, the voice agent will call the send_to_ppt_agent tool to send the feedbacks to the ppt agent and get the success or failure back to the voice agent quickly.if they have other feedbacks, the voice agent will call the send_to_ppt_agent tool again to send the feedbacks again to the ppt agent and get the success or failure back to the voice agent quickly.if they don't have other feedbacks, the voice agent will stop ask for new feedbacks.
Notice:this tool will return the success or failure quickly,and will not wait for the ppt agent to generate the ppt.so the response data is just a message of if the data is sent to the ppt agent successfully.

for instance:
```json
{
    "from": "voice_agent",
    "to": "ppt_agent",
    "data": "people have some critical feedbacks to the ppt, the feedbacks are: the font should be bigger, the color should be more colorful",
}
```

```json

```json
{
    "code": 200,
    "message": "success",
    "data": null,
}
```

---

#### 1.4 Get api/v1/fetch_from_ppt_message_queue

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

the user prompt of the voice agent will record if the ppt_message_queue is not empty in real time. when the user interrupts the voice agent while it is still speaking (vad_start fires during assistant output), and the queue is not empty when vad_end, the context will be like:

</interrupted>
<status>not empty</status>
<user>xxxxx</user>

if the assistant is idle and the user speaks, and the queue is empty when vad_end, the context will be like:

<status>empty</status>
<user>xxxxx</user>

and voice agent will depend if the queue is not empty to judge if call the fetch_from_ppt_message_queue tool to fetch the data from the queue or not.

go:
```go
func fetch_from_ppt_message_queue(ctx context.Context, args map[string]string) (string, error) {
    // fetch ALL messages from the ppt message queue at once
    // success (has messages): plain string with all messages concatenated by " | "
    return "the ppt message is: xxxx,xxxx... | yyyy,yyyy...", nil
    // success (empty): plain string telling it's empty
    return "queue is empty", nil
    // failure: plain string error
    return "failed to fetch the data from the ppt message queue", errors.New("failed to fetch the data from the ppt message queue") or ctx.Err()
}
```

tool result examples:
- success (single message): `the ppt message is: the new version of the ppt is generated successfully`
- success (multiple messages): `the ppt message is: the new version of the ppt is generated successfully | questions for user: 请问封面的主色调有什么偏好吗？`
- success (empty): `queue is empty`
- failure: `failed to fetch the data from the ppt message queue`

---

#### 1.5 Post api/v1/start_conversation

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

the frontend will start the conversation,call this api,and the vad detection,noise suppression,acoustic echo cancellation will be started.the voice agent will start the conversation once frontend call this api.

---

### module 2: ppt agent

#### 2.1 system prompt

every time the ppt agent runtime starts a new llm turn (except the very first passive restart after being idle), the backend rebuilds the system message so it always contains the **real-time voice message queue status**.

example system prompt:
```text
You are a PPT generation agent. Use the available tools to create the presentation.
You must write the slide content to a Markdown file (e.g. slides.md) using Slidev syntax,
then use execute_command to run `npx slidev export slides.md --output ppt.pdf` to produce the final PDF.
After the PDF is successfully exported, you MUST call send_to_voice_agent to notify the voice agent.
Current voice message queue status: has 2 pending message(s).
If the queue has messages, call fetch_from_voice_message_queue to consume them.
```

notice:
- the agent **does not need to wait** for the voice agent to confirm before continuing its work.
- the agent decides on its own when to pause (e.g. after sending a message via `send_to_voice_agent`).
- the agent decides on its own when to fetch new feedback via `fetch_from_voice_message_queue`.

---

#### 2.2 some tools:

```go
func edit_file(ctx context.Context, path string, old_string string, new_string string) error // will edit the file
func write_file(ctx context.Context, path string, content string) error // will overwrite the file
func read_file(ctx context.Context, path string) (string, error) // will read the file
func list_dir(ctx context.Context, path string) ([]string, error) // will list the directory
func move_file(ctx context.Context, src, dst string) error // will move the file
func execute_command(ctx context.Context, command string, workdir string) (stdout string, stderr string, err error) // will execute the command
```

---

#### 2.3 Post api/v1/send_to_voice_agent

request body:
```json
{
    "from": "ppt_agent",
    "to": "voice_agent",
    "data": "string",
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
    "message": "failed to send the data to the voice agent",    
    "data": null,
}
```

ppt agent is generating the new version of the ppt. when it wants to notify the voice agent (e.g. "the new version of the ppt is generated successfully"), it calls the `send_to_voice_agent` tool. this tool only enqueues the message into the `ppt_message_queue`; it **does not stop the ppt agent runtime**. the ppt agent decides on its own when to pause or continue working.

Notice:this tool will return the success or failure quickly,and will not wait for the voice agent to receive the message.so the response data is just a message of if the data is enqueued successfully.

the send_to_voice_agent tool function definition (PPT agent uses standard OpenAI function calling):
LLM:
```json
{
  "name": "send_to_voice_agent",
  "arguments": {"data": "..."}
}
```
go:
```go
func send_to_voice_agent(ctx context.Context, data string) (string, error) {
    // enqueue the message to the voice agent
    // success: plain string confirmation
    return "data is sent to the voice agent successfully", nil
    // failure: plain string error
    return "failed to send the data to the voice agent", errors.New("failed to send the data to the voice agent") or ctx.Err()
}
```

for instance:
```json
{
    "from": "ppt_agent",
    "to": "voice_agent",
    "data": "the new version of the ppt is generated successfully",
}
```

```json
{
    "code": 200,
    "message": "success",
    "data": null,
}
```

---

#### 2.3 PPT agent runtime lifecycle

the backend maintains a background goroutine (runtime) for the ppt agent. the runtime runs a loop:

1. **refresh system prompt**: before every llm call, the backend rebuilds the system message so it always contains the **real-time voice message queue status** (e.g. "queue is empty" or "queue has 2 pending messages").
2. **llm inference**: call the llm with the current history. because the system prompt tells the queue status, the ppt agent can decide whether to call `fetch_from_voice_message_queue` to consume feedback.
3. **after the llm turn finishes**:
   - if the `voice_message_queue` has new messages, the runtime keeps running. on the next loop iteration it refreshes the system prompt (which now reports the real-time queue status) and calls the llm again, so the ppt agent can decide on its own when to call `fetch_from_voice_message_queue` to consume the feedback.
   - if the queue is empty, the runtime exits (goes idle). it will be restarted later when a new voice message arrives.

**voice message arrival (`send_to_ppt_agent`)**:
- the message is always enqueued into the `voice_message_queue` first.
- if the ppt agent runtime is **already running**, the runtime will notice the queue on its next loop iteration; no cancel/restart happens.
- if the ppt agent runtime is **idle**, the backend drains the entire queue, appends the messages to the ppt agent history as user prompts, and starts the runtime again.

this means the ppt agent is never forcibly interrupted while it is working. it only stops when it finishes a turn and finds the queue empty.

---

#### 2.4 fetch_from_voice_message_queue tool

the ppt agent can call this tool to explicitly fetch the next pending message from the `voice_message_queue`.

LLM (PPT agent uses standard OpenAI function calling):
```json
{
  "name": "fetch_from_voice_message_queue",
  "arguments": {}
}
```

go:
```go
func fetch_from_voice_message_queue(ctx context.Context) (string, error) {
    // fetch the next message from the voice message queue
    // success (has message): plain string with the feedback
    return "the feedback is: xxxx,xxxx...", nil
    // success (empty): plain string telling it's empty
    return "queue is empty", nil
}
```

---

### module 3: kb service

#### 3.1 Post api/v1/kb/query-chunks（同步）

request body:
```json
{
    "from": "ppt_agent",
    "to": "kb_service",
    "query": "string",
}
```

response body:
```json
{
    "code": 200,
    "message": "success",
    "data": {
        "chunks": [
            {
                "chunk_id": "string",
                "content": "string",
            }
        ],
        "total": "int",
    },
}
```

if failed:
```json
{
    "code": 400,
    "message": "failed to query the chunks from the kb service",   
    "data": null,
}
```

go:

```go
type chunk struct {
    chunk_id: "string",
    content: "string",
}
```

```go
func query_chunks(ctx context.Context, query string) ([]chunk, int, error) {
    // query the chunks from the kb service
    // success: returns chunk list and total count
    return []chunk, total, nil
    // failure: returns empty list, 0, and a plain error string
    return []chunk{}, 0, errors.New("failed to query the chunks from the kb service") or ctx.Err()
}
```

if ppt agent want to query the chunks from the kb service,it can call the query_chunks tool,that tool will call this api to query the chunks from the kb service and get the chunks and the total back to the ppt agent slowly(blockingly).

---

### module 4: search service

#### 4.1 Post api/v1/search/query

request body:
```json
{
    "from": "ppt_agent",
    "to": "search_service",
    "query": "string",
}
```

response body:  
```json
{
    "code": 200,
    "message": "success",
    "data": "string",
}
```

if failed:
```json
{
    "code": 400,
    "message": "failed to search the web",
    "data": null,
}
```

if ppt agent want to search the web,it can call the search_web tool,that tool will call this api to search the web and get the result back to the ppt agent slowly(blockingly).the result is the summary of the search result.

go:
```go
func search_web(ctx context.Context, query string) (string, error) {
    // search the web
    // success: returns a plain string summary of the search result
    return "the summary of the search result is xxxx,xxxx...", nil
    // failure: returns a plain string error
    return "failed to search the web", errors.New("failed to search the web") or ctx.Err()
}
```

---

## frontend api

the frontend will written by ts and react.we will use the ability of web browser to implement the frontend,such as vad_detection,acoustic echo cancellation,noise suppression,etc.

### frontend interrupt handling responsibility (critical)

data flow recap: the backend streams tokens to the frontend. the frontend buffers them and feeds the tts engine sentence by sentence (triggered by punctuation). therefore:
- "already spoken" = text the tts engine has actually finished playing before vad_start. if a sentence is cut off mid-playback, only the part that was actually spoken is kept.
- "not yet spoken" = everything else: text the frontend has buffered but not yet pushed to tts, plus the tail of any sentence that was pushed to tts but not finished playing.
- "still being generated by the backend" = text not yet received by the frontend.

when vad_start fires, the frontend does the following in order:

1. fast interrupt check: send the audio from vad_start to min(vad_end, vad_start + 1.5s) to `POST /api/v1/voice/vad_start`. the backend runs a quick asr + interrupt-check llm and returns `{"interrupt": true | false}`.
2. if `interrupt: false`:
   - the frontend does nothing. if the assistant is active, tts continues playing as if nothing happened.
   - when vad_end arrives, the frontend still sends the full audio to `POST /api/v1/voice/vad_end`. this is mostly a formality to keep the frontend logic uniform: the backend simply looks up the already-computed fast-check result and immediately returns `{"ignored": true}`. no llm inference is triggered and the conversation history is unchanged.
3. if `interrupt: true`:
   - if the assistant turn is active, stop local playback: clear the tts queue and stop any audio currently playing.
   - tell the backend to cancel: the backend aborts the ongoing llm inference.
   - buffer the new audio locally until vad_end. do not append anything to the llm context yet.
   - wait if action has started: if the backend stream has already emitted the opening `<` of the first `<action`, the frontend must let that goroutine run until the **full action sequence** is complete. actions are silent, so the user will not feel "the machine is still talking".
   - when vad_end arrives, send the full audio to `POST /api/v1/voice/vad_end` together with `needs_interrupted_prefix: true | false`. **this flag is decided by the frontend**: it is `true` only when the assistant was still playing **tts text** to the user (i.e. the tts queue was not empty or the stream had not yet emitted any `<action>`). if the tts had already finished and the assistant had entered the silent action phase, the flag is `false`.
   - the backend runs full asr and calls the voice agent llm. it prepends `</interrupted>\n` to the user message **only when `needs_interrupted_prefix` is `true`**.
   - decide what goes into history:
     - assistant message: the text that had **already been spoken** (it may be a complete sentence or a truncated half-sentence). if the stream had already entered the action phase, also append the **complete action sequence** (`<action>...</action>`) in order.
     - tool messages (role = `tool`): for each action that was emitted, append its synchronous execution result as a plain string. these follow the assistant message.
     - user message (role = `user`): the backend emits a `user_transcript` SSE chunk containing the fully formatted user message. the frontend appends it to the local history as `role: 'user'`.
   - the overall turn order appended to history is: `assistant` (truncated spoken text + complete action sequence) → `tool` messages → `[optional] assistant` (post-action TTS if the model produced any). **the post-action TTS is a separate assistant message, not merged into the first assistant message.**

edge case: if the backend stream hangs abnormally, the frontend may truncate any trailing incomplete action before appending the cached user input.
