
## module 1 : voice agent

### 1.1 Post api/v1/update_requirements

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

### 1.2 Post api/v1/require_confirm


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

### 1.3 Post api/v1/send_to_ppt_agent

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

### 1.4 Get api/v1/get_messages_from_ppt_agent

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

### 1.5 Post api/v1/start_conversation

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

