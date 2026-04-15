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
```json
{
    "audio": "base64-encoded audio from vad_start to vad_end",
    "format": "pcm"
}
```

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
| tts token | `{"type": "tts", "text": "好的"}` | a piece of tts text. the frontend feeds these tokens to the tts engine |
| action | `{"type": "action", "payload": "update_requirements|topic:数学"}` | a parsed action extracted from the llm stream |
| turn end | `{"type": "turn_end"}` | signals the end of this assistant turn |

note: these two endpoints are declared here but not yet implemented in the current mvp backend code. the current backend only exposes the http tool apis documented below.

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
<action>update_requirements|topic:...|description:...|total_pages:...|audience:...</action>
```
go：
```go
func update_requirements(ctx context.Context, requirements map[string]any) (string, error) {
    // update the requirements fields in the backend
    // return the missing fields after some fields are updated
    return "we now still missing xxx,xxx,xxx ...", nil
    or
    return "all fields are updated", nil
    or
    return "failed to update the requirements,please try again", errors.New("failed to update the requirements,please try again") or ctx.Err()  
}
```

LLM -> parse the fields and their value,make the map[string]any,and call the update_requirements tool function->get the return value quickly -> LLM -> ask the user to provide the missing fields.
if all fields are updated, LLM will call the require_confirm tool to ask the user to confirm the requirements.

the update_requirements tool will disappear forever after the first send_to_voice_agent tool is called.

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
    return "data is sent to the frontend successfully", nil
    or
    return "failed to send the data to the frontend", errors.New("failed to send the data to the frontend") or ctx.Err()
}
```

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
    return "data is sent to the ppt agent successfully", nil
    or
    return "failed to send the data to the ppt agent", errors.New("failed to send the data to the ppt agent") or ctx.Err()
}
```

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
func fetch_from_ppt_message_queue(ctx context.Context) (string, bool, error) {
    // fetch the data from the ppt message queue
    return "the ppt message is xxxx,xxxx...", true, nil
    or
    return "queue is empty", false, nil
    or
    return "failed to fetch the data from the ppt message queue", false, errors.New("failed to fetch the data from the ppt message queue") or ctx.Err()
}
```

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

#### 2.1 some tools:

```go
func edit_file(ctx context.Context, path string, old_string string, new_string string) error // will edit the file
func write_file(ctx context.Context, path string, content string) error // will overwrite the file
func read_file(ctx context.Context, path string) (string, error) // will read the file
func list_dir(ctx context.Context, path string) ([]string, error) // will list the directory
func move_file(ctx context.Context, src, dst string) error // will move the file
func execute_command(ctx context.Context, command string, workdir string) (stdout string, stderr string, err error) // will execute the command
```

---

#### 2.2 Post api/v1/send_to_voice_agent

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

ppt agent is generating the new version of the ppt,and when the ppt is finished, the ppt agent will call the send_to_voice_agent tool,that tool will call this api to notice the voice agent that the new version of the ppt is generated successfully and get the success or failure back to the ppt agent quickly.
Notice:this tool will return the success or failure quickly,and will not wait for the voice agent to notice the new version of the ppt is generated successfully.so the response data is just a message of if the data is sent to the voice agent successfully.

the send_to_voice_agent tool function definition:
LLM:
```text
<action>send_to_voice_agent|data:...</action>
```
go:
```go
func send_to_voice_agent(ctx context.Context, data string) (string, error) {
    // send the data to the voice agent
    return "data is sent to the voice agent successfully", nil
    or
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

when ppt is being generated, **though the new version of the ppt is not finished, the voice agent may send some feedbacks to the ppt agent via api/v1/send_to_ppt_agent(send_to_voice_agent tool).** if so,we will stop all the tools of ppt agent via ctx.cancel() and stop the ppt agent runtime(goroutine),and then we will add the feedbacks in queue(the send_to_ppt_agent and send_to_voice_agent are both sending data to the voice_message_queue(the send_to_ppt_agent) or ppt_message_queue(send_to_voice_agent) which will be maintained by the backend program.) to the history of the ppt agent(a new user prompt),and then we will start a new ppt agent runtime(goroutine) to generate the new version of the ppt,if ppt agent is confused by the feedbacks,it can use send_to_voice_agent tool to send the questions to the voice agent,and voice agent next time call the fetch_from_ppt_message_queue tool to get those questions,the voice agent will ask the user to answer the questions,and then the voice agent will call the send_to_ppt_agent tool to send the answers to the ppt agent and get the success or failure back to the voice agent quickly.and in this process,the ppt agent will be canceled until the next send_to_ppt_agent tool is called.(namely,the ppt agent will stoped if it calls the send_to_voice_agent tool,and restart until the next send_to_ppt_agent tool is called.)the ppt agent has no Post api/v1/fetch_from_voice_message_queue api because the voice agent will fetch the data from the voice_message_queue directly to append it to the history of the voice agent(a new user prompt).

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
    return []chunk, total, nil
    or
    return []chunk, 0, errors.New("failed to query the chunks from the kb service") or ctx.Err()
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
    return "the summary of the search result is xxxx,xxxx...", nil
    or
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
   - when vad_end arrives, send the full audio to `POST /api/v1/voice/vad_end`. the backend runs full asr. however, the voice agent llm inference cannot start until both (1) the action sequence is fully resolved and (2) the full asr transcript is ready.
   - decide what goes into history:
     - assistant message: the text that had **already been spoken** (it may be a complete sentence or a truncated half-sentence), plus the **complete action sequence** only if the stream had already entered the action phase. if the stream was cancelled before any `<` was emitted, there is no action.
     - user message (role = `user`):
       - if the assistant was still streaming when vad_start fires: `{"role": "user", "content": "</interrupted>\n<status>...</status>\n<user>...</user>"}`
       - if the assistant had already finished its turn (tts fully played, stream ended) and the user simply spoke again: `{"role": "user", "content": "<status>...</status>\n<user>...</user>"}` (no `</interrupted>`)
   - send the rebuilt context to the llm: assistant message first, then the user message.

edge case: if the backend stream hangs abnormally, the frontend may truncate any trailing incomplete action before appending the cached user input.
