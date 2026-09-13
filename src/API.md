
## 1.1 POST api/v1/frontend/vad_engine/vad_start

request body:

```json
{
    "type": "vad_engine/vad_start_request",
    "id": str,
    "timestamp": str
}
```

response body:

```json
{
    "type":"vad_engine/vad_start_response",
    "id": str,
    "timestamp": str,
    "code": 200,
    "data": {
        "said_words": str,
        "raw_the_words_left_unsaid": str,
        "llm_state": str 
    }
}
```

## 1.2 POST api/v1/frontend/vad_engine/vad_end

request body:

```json
{
    "type": "vad_engine/vad_end_request",
    "id": str,
    "timestamp": str
}
```

response body:

```json
{
    "type":"vad_engine/vad_end_response",
    "id": str,
    "timestamp": str,
    "code": 200,
    "data": {
        "prefix_audio": str,
        "audio": str
    }
}
```

## 2.1 WS api/v1/backend/tts_engine/infer

request body:

```json
{
    "type": "tts_engine/infer_request",
    "id": str,
    "timestamp": str,
    "data": {
        "input_token": str,
        "state": str,
        "format": "pcm16",
        "sample_rate": int
    }
}
```

response body:

```json
{
    "type": "tts_engine/infer_response/audio_header",
    "id": str,
    "timestamp": str,
    "data": {
        "format": "pcm16",
        "sample_rate": int,
        "channels": int
    }
}
```

```json
pcm16le: int16 × N，little-endian，byte count % 2 == 0
```

```json
{
    "type": "tts_engine/infer_response/finished",
    "id": str,
    "timestamp": str,
    "code": 200
}
```

## 3.1 POST api/v1/backend/asr_engine/infer

request body:

```json
{
    "type": "asr_engine/infer_request",
    "id": str,
    "timestamp": str,
    "data": {
        "prefix_audio": str,
        "audio": str,
        "format": "pcm16",
        "sample_rate": int
    }
}
```

response body:

```json
{
    "type":"asr_engine/infer_response",
    "id": str,
    "timestamp": str,
    "code": 200,
    "data": {
        "transcribed_words": str
    }
}
```

## 4.1 SSE POST api/v1/backend/llm_engine/infer

request body:

```json
{
    "type": "llm_engine/infer_request",
    "id": str,
    "timestamp": str,
    "data": {
        "prompt": str
    }
}
```

response body:

```json
{"type":"llm_engine/infer_response","id": str,"code": 200,"data":{"token": str,"state": "inferring_tts_tokens"}}
{"type":"llm_engine/infer_response","id": str,"code": 200,"data":{"token": str,"state": "inferring_tool_call_tokens"}}
{"type":"llm_engine/infer_response","id": str,"code": 200,"data":{"token": "","state": "idle"}}
```

## 4.2 POST api/v1/backend/session/interrupt

request body:

```json
{
    "type": "session/interrupt_request",
    "id": str,
    "timestamp": str,
    "data": {
        "said_words": str
    }
}
```

response body:

```json
{
    "type":"session/interrupt_response",
    "id": str,
    "timestamp": str,
    "code": 200,
    "data": {
        "raw_the_words_left_unsaid": str,
        "llm_state": str
    }
}
```