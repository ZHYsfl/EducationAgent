api.md

we follow the mvp rule to build things fast and iteratively.

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
@{update_requirements|topic:...|description:...|total_pages:...|audience:...}
```
go：
```go
func update_requirements(requirements map[string]any) (string, error) {
    // update the requirements fields in the backend
    // return the missing fields after some fields are updated
    return "we now still missing xxx,xxx,xxx ...", nil
    or
    return "all fields are updated", nil
    or
    return "failed to update the requirements,please try again", errors.New("failed to update the requirements,please try again")
}
```

LLM -> parse the fields and their value,make the map[string]any,and call the update_requirements tool function->get the return value quickly -> LLM -> think and ask the user to provide the missing fields.
if all fields are updated, LLM will call the require_confirm tool to ask the user to confirm the requirements.

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
@{require_confirm}
```
go:
```go
func require_confirm(requirements map[string]any) (string, error) {
    // require the user to confirm the requirements
    return "data is sent to the frontend successfully", nil
    or
    return "failed to send the data to the frontend", errors.New("failed to send the data to the frontend")
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
@{send_to_ppt_agent|data:...}
```
go:
```go
func send_to_ppt_agent(data string) (string, error) {
    // send the data to the ppt agent
    return "data is sent to the ppt agent successfully", nil
    or
    return "failed to send the data to the ppt agent", errors.New("failed to send the data to the ppt agent")
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

### module 2: ppt agent

#### 2.1 Post api/v1/



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
@{send_to_voice_agent|data:...}
```
go:
```go
func send_to_voice_agent(data string) (string, error) {
    // send the data to the voice agent
    return "data is sent to the voice agent successfully", nil
    or
    return "failed to send the data to the voice agent", errors.New("failed to send the data to the voice agent")
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

when ppt is being generated, **though the new version of the ppt is not finished, the voice agent may send some feedbacks to the ppt agent via api/v1/send_to_ppt_agent(send_to_voice_agent tool).** if so,we will stop 