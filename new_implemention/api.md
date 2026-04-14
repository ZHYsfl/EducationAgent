api.md

we follow the mvp rule to build things fast and iteratively.all the tools have context.Context as the first argument.the backend program is deployed in a docker sandbox.the sandbox has node.js,go,slidev installed.the voice agent llm will be finetined by us,and the ppt agent llm will directly use the sota llm api.

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

LLM -> parse the fields and their value,make the map[string]any,and call the update_requirements tool function->get the return value quickly -> LLM -> think and ask the user to provide the missing fields.
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

#### 1.4 Post api/v1/fetch_from_ppt_message_queue

request body:
```json
{
    "from": "voice_agent",
}
```

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

the user prompt of the voice agent will record if the ppt_message_queue is not empty in real time,and when user interrupt the voice agent,and the queue is not empty when vad_end,the context will be like:

</interrupted>
<status>not empty</status>
<user>xxxxx</user>

if user say in idle status of voice_agent,and the queue is empty when vad_end,the context will be like:

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

when ppt is being generated, **though the new version of the ppt is not finished, the voice agent may send some feedbacks to the ppt agent via api/v1/send_to_ppt_agent(send_to_voice_agent tool).** if so,we will stop all the tools of ppt agent via ctx.cancel() and stop the ppt agent runtime(goroutine),and then we will add the feedbacks in queue(the send_to_ppt_agent and send_to_voice_agent are both sending data to the voice_message_queue(the send_to_ppt_agent) or ppt_message_queue(send_to_voice_agent) which will be maintained by the backend program.) to the history of the ppt agent(a new user prompt),and then we will start a new ppt agent runtime(goroutine) to generate the new version of the ppt,if ppt agent is confused by the feedbacks,it can use send_to_voice_agent tool to send the questions to the voice agent,and voice agent next time call the fetch_from_ppt_message_queue tool to get those questions,the voice agent will ask the user to answer the questions,and then the voice agent will call the send_to_ppt_agent tool to send the answers to the ppt agent and get the success or failure back to the voice agent quickly.and in this process,the ppt agent will be canceled until the next send_to_ppt_agent tool is called.(namely,the ppt agent will stoped if it calls the send_to_voice_agent tool,and restart until the next send_to_ppt_agent tool is called.)

