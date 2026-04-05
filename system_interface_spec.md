# Multimodal AI Teaching Agent - Full System Interface Specification v1.0

> **Document Nature**: This document serves as the "legal" basis for communication among all modules. The implementation of any module must strictly comply with the data structures, HTTP API signatures, timing constraints, and error codes defined in this document. If any implementation conflicts with this document, this document shall prevail.  
> **Programming Language**: Go (used uniformly across the entire system)  
> **Last Updated**: 2026-03-19

---

## Table of Contents

- [Chapter 0 Global Conventions](#chapter-0-global-conventions)
- [Chapter 1 Asynchronous Message Bus - Context Injection Queue](#chapter-1-asynchronous-message-bus---context-injection-queue)
- [Chapter 2 Voice Agent ↔ Frontend](#chapter-2-voice-agent--frontend)
- [Chapter 3 Voice Agent ↔ PPT Agent](#chapter-3-voice-agent--ppt-agent)
- [Chapter 4 Knowledge Base Service](#chapter-4-knowledge-base-service)
- [Chapter 5 Memory Service](#chapter-5-memory-service)
- [Chapter 6 Web Search Service](#chapter-6-web-search-service)
- [Chapter 7 Database Service](#chapter-7-database-service)
- [Chapter 8 Reference Material Processing Pipeline](#chapter-8-reference-material-processing-pipeline)
- [Appendix A Team Responsibilities](#appendix-a-team-responsibilities)
- [Appendix B System Architecture Diagram](#appendix-b-system-architecture-diagram)

---

## Chapter 0 Global Conventions

This chapter defines the foundational conventions that all modules must follow. Unless otherwise stated, all chapters inherit the rules defined here.

### 0.1 Unified Response Format

All HTTP JSON API responses use the following unified wrapper format (the three-part structure is recommended; `message` may be omitted in simplified internal scenarios):

```go
type APIResponse struct {
    Code    int             `json:"code"`              // Business status code; 200 indicates success
    Message string          `json:"message,omitempty"` // Human-readable status description (optional)
    Data    json.RawMessage `json:"data,omitempty"`    // Business data; may be empty on failure
}
```

**Success Example:**

```json
{
  "code": 200,
  "message": "success",
  "data": { "task_id": "task_abc123" }
}
```

**Failure Example:**

```json
{
  "code": 40001,
  "message": "parameter topic cannot be empty",
  "data": null
}
```

### 0.2 Unified Error Code System


| Range | Category | Description |
| ------------- | ------- | ---------------------------- |
| `200` | Success | Request completed successfully |
| `40001-40099` | Parameter Error | Missing required fields, type mismatch, invalid format |
| `40100-40199` | Authentication / Authorization | Token expired, no permission |
| `40400-40499` | Resource Not Found | Target entity does not exist |
| `40900-40999` | Conflict | Resource state conflict (e.g., duplicate creation, operation not allowed while suspended) |
| `50000-50099` | Internal Service Error | Unexpected exception |
| `50200-50299` | Dependency Service Unavailable | Downstream timeout or crash of LLM / Redis / vector database, etc. |


Each module may assign specific codes within the above ranges, but must not use codes outside its designated range.

### 0.3 Authentication Method

- All external HTTP requests (frontend → backend) carry JWT: `Authorization: Bearer <token>`
- Internal HTTP calls between modules (e.g., Voice Agent → PPT Agent) carry a shared secret: `X-Internal-Key: <key>`
- WebSocket connections pass the token in the URL query parameter: `/ws?token=<jwt>`

```go
type JWTClaims struct {
    UserID    string `json:"user_id"`
    Username  string `json:"username"`
    ExpiresAt int64  `json:"exp"`
}
```

### 0.4 ID Conventions

All entity IDs use UUID v4 with a type prefix to improve readability and debugging efficiency:


| Entity | Prefix | Example |
| ------ | --------- | --------------------- |
| User | `user_` | `user_a1b2c3d4-...` |
| Session | `sess_` | `sess_e5f6g7h8-...` |
| PPT Task | `task_` | `task_abc123de-...` |
| PPT Page | `page_` | `page_f9e8d7c6-...` |
| File | `file_` | `file_11223344-...` |
| KB Collection | `coll_` | `coll_aabbccdd-...` |
| KB Document | `doc_` | `doc_55667788-...` |
| Text Chunk | `chunk_` | `chunk_99aabb00-...` |
| Memory Entry | `mem_` | `mem_ccddeeff-...` |
| Search Request | `search_` | `search_12345678-...` |
| Context Message | `ctx_` | `ctx_deadbeef-...` |


Generator function signature:

```go
func NewID(prefix string) string  // Example: NewID("task_") → "task_a1b2c3d4-e5f6-7890-abcd-ef1234567890"
```

### 0.5 Task Resolution Rules (Multiple PPT Tasks Scenario)

One `task_id` corresponds to one PPT. The same user may have multiple tasks at the same time. The Voice Agent must determine which task the user's speech refers to according to the following priority:

```text
1. reply_to_context_id matches → use the task_id bound to that context (conflict-reply scenario)
2. The utterance explicitly mentions a task name/number → use the matched task_id
3. active_task_id exists     → use the current active task (default)
4. Only 1 available task     → use that task_id
5. None of the above         → ask the user to choose, never guess
```

**`active_task_id` Maintenance Rules:**

| Event | Action |
|---|---|
| User creates a new PPT (`ppt/init` succeeds) | Set to the new `task_id` |
| Frontend sends `page_navigate` to switch task | Update to that `task_id` |
| User explicitly switches by voice ("open that advanced math courseware") | Update after LLM parsing |
| Current task enters `completed` / `failed` | Clear it and wait for the next explicit selection |

Session struct extension:

```go
type Session struct {
    // ... existing fields ...
    ActiveTaskID string            // Current active task
    PendingQuestions map[string]string // context_id → task_id mapping
}
```

### 0.6 Timestamps

- All timestamp fields use **Unix milliseconds** (`int64`)
- JSON field names end with `_at` or `timestamp`
- In Go, get the current millisecond timestamp via: `time.Now().UnixMilli()`

### 0.7 General HTTP Rules

- Content-Type: `application/json` (except for file uploads, which use `multipart/form-data`)
- Request method semantics: `POST` = create/action, `GET` = query, `PUT` = full update, `PATCH` = partial update, `DELETE` = delete
- List queries support pagination: `?page=1&page_size=20`, and the response includes a `total` field
- Default port allocation:


| Module | Default Port |
| ---------------------- | ---- |
| Voice Agent | 9000 |
| PPT Agent | 9100 |
| Knowledge Base Service | 9200 |
| Memory Service | 9300 |
| Web Search Service | 9400 |
| Database Service | 9500 |


---

## Chapter 1 Asynchronous Message Bus - Context Injection Queue

### 1.1 Design Philosophy

The Voice Agent is the central orchestrator of the system and is responsible for real-time conversations with the user. To ensure user experience, the Voice Agent **never blocks waiting for** responses from external modules (Knowledge Base, Web Search, Memory, PPT Agent). All outbound requests are initiated asynchronously through goroutines, and all results are collected into a single `contextQueue` channel. The Voice Agent reads from this queue at the proper time and injects the results into the LLM context.

This design is directly inspired by the "listen while thinking" concept in the SEAL architecture: LLM processing speed (100+ tokens/s) is much faster than speech I/O (5 tokens/s), so idle time can be fully utilized to provide the LLM with additional context.

### 1.2 Core Data Structure

```go
type ContextMessage struct {
    ID        string            `json:"id"`         // Unique ID, format ctx_<uuid>
    Source    string            `json:"source"`      // Source module of the message
    Priority  string            `json:"priority"`    // Priority
    MsgType   string            `json:"msg_type"`    // Message type
    Content   string            `json:"content"`     // Text injected into the LLM context
    Metadata  map[string]string `json:"metadata"`    // Additional metadata
    Timestamp int64             `json:"timestamp"`   // Creation time (Unix ms)
}
```

**Source Enum:**


| Value | Description |
| ---------------- | ------------------------- |
| `knowledge_base` | From knowledge-base RAG retrieval results |
| `web_search` | From web search results |
| `memory` | From memory module (user profile / historical summary) |
| `ppt_agent` | From PPT Agent (status update / conflict assistance request) |


**Priority Enum and Handling Rules:**


| Value | Behavior |
| -------- | -------------------------------------------------------------------------------------------- |
| `high` | Process immediately. If the Voice Agent is currently Speaking, wait until the current sentence finishes playing, then insert playback; if it is Idle/Listening, trigger playback directly. Typical scenario: PPT Agent conflict assistance request. |
| `normal` | Inject into the system prompt context before the next LLM call. Typical scenarios: RAG retrieval results, search results, user memory. |
| `low` | Cache in the local buffer pool and inject only when Idle and no messages are pending. Typical scenarios: background preloaded knowledge, PPT rendering progress notifications. |


**MsgType Enum:**


| Value | Description | Typical Source |
| ------------------- | --------------------- | ---------------- |
| `rag_chunks` | Knowledge chunks hit by RAG retrieval | `knowledge_base` |
| `search_result` | Web search summary | `web_search` |
| `user_profile` | User profile information | `memory` |
| `memory_recall` | Relevant historical memory | `memory` |
| `ppt_status` | PPT task status change notification | `ppt_agent` |
| `conflict_question` | PPT Agent conflict assistance request requiring voice playback | `ppt_agent` |
| `tool_result` | Tool call result (fallback type, used only when it cannot be classified) | Any |


**MsgType Selection Rules (Mandatory):**

1. **Prefer dedicated types**: `rag_chunks`, `search_result`, `user_profile`, `memory_recall`, `ppt_status`, `conflict_question`
2. **Use `tool_result` only when classification is impossible**: for example, a newly integrated tool does not yet have a dedicated MsgType, or during a temporary debugging phase
3. **Recommended practice for this project**: KB uses `rag_chunks`; Web Search uses `search_result`; Memory uses `user_profile` / `memory_recall`; PPT Agent uses `ppt_status` / `conflict_question`

### 1.3 Internal Integration in Voice Agent

#### 1.3.1 Pipeline Extension

Add the following fields to the existing `Pipeline` struct:

```go
type Pipeline struct {
    // ... keep existing fields unchanged ...

    contextQueue    chan ContextMessage      // Unified context injection queue, capacity 64
    pendingContexts []ContextMessage         // Drained contexts waiting for injection
    pendingMu       sync.Mutex

    highPriorityQueue chan ContextMessage     // Dedicated high-priority channel, capacity 16
}
```

#### 1.3.2 Context Drain Mechanism

Before `startProcessing()` calls the LLM, perform a non-blocking drain:

```go
func (p *Pipeline) drainContextQueue() []ContextMessage {
    var msgs []ContextMessage
    for {
        select {
        case msg := <-p.contextQueue:
            msgs = append(msgs, msg)
        default:
            return msgs
        }
    }
}
```

Format drained messages into an appended segment for the LLM system prompt:

```go
func FormatContextForLLM(msgs []ContextMessage) string {
    if len(msgs) == 0 {
        return ""
    }
    var sb strings.Builder
    sb.WriteString("\n\n[Additional System Information - The following materials were retrieved in the background and are provided for reference in answering]\n")
    for _, m := range msgs {
        sb.WriteString(fmt.Sprintf("\n--- Source: %s | Type: %s ---\n%s\n", m.Source, m.MsgType, m.Content))
    }
    return sb.String()
}
```

#### 1.3.3 High-Priority Listener Goroutine

A dedicated goroutine listens to `highPriorityQueue`. After receiving a message:

1. If `MsgType == "conflict_question"`, immediately trigger the TTS playback flow
2. Record `Metadata["context_id"]` into the Session's pending-answer list
3. On the user's next utterance, the Voice Agent LLM determines whether the user is answering this question; if so, route it to PPT Agent together with `context_id`

```go
func (p *Pipeline) highPriorityListener(ctx context.Context) {
    for {
        select {
        case msg := <-p.highPriorityQueue:
            switch msg.MsgType {
            case "conflict_question":
                ttsText := msg.Content
                p.session.SetState(StateSpeaking)
                sentenceCh := make(chan string, 1)
                sentenceCh <- ttsText
                close(sentenceCh)
                p.ttsWorker(ctx, sentenceCh)
                p.session.SetState(StateIdle)
                p.session.AddPendingQuestion(msg.Metadata["context_id"], msg.Metadata["page_id"])
            default:
                p.pendingMu.Lock()
                p.pendingContexts = append(p.pendingContexts, msg)
                p.pendingMu.Unlock()
            }
        case <-ctx.Done():
            return
        }
    }
}
```

### 1.4 Asynchronous Request Dispatch Pattern

The unified pattern for the Voice Agent to send requests to external modules is "fire-and-forget with callback":

```go
func (p *Pipeline) asyncQuery(
    ctx context.Context,
    source string,
    msgType string, // Prefer passing a dedicated type; automatically falls back to tool_result if empty
    queryFn func() (string, error),
) {
    go func() {
        result, err := queryFn()
        if err != nil {
            log.Printf("[ContextBus] %s query failed: %v", source, err)
            return
        }
        if result == "" {
            return
        }
        resolvedType := msgType
        if resolvedType == "" {
            resolvedType = "tool_result"
        }
        msg := ContextMessage{
            ID:        NewID("ctx_"),
            Source:     source,
            Priority:  "normal",
            MsgType:   resolvedType,
            Content:   result,
            Timestamp: time.Now().UnixMilli(),
        }
        select {
        case p.contextQueue <- msg:
        case <-ctx.Done():
        }
    }()
}
```

Example calls (recommended):

```go
// Knowledge base results
p.asyncQuery(ctx, "knowledge_base", "rag_chunks", queryKB)

// Memory recall results
p.asyncQuery(ctx, "memory", "memory_recall", recallMemory)

// Web search results
p.asyncQuery(ctx, "web_search", "search_result", queryWeb)

// Fallback (only when it cannot be classified)
p.asyncQuery(ctx, "web_search", "", queryUnknownTool)
```

### 1.5 Sequence Diagram: Full Asynchronous Context Injection Flow

```
User speaks ──→ VAD triggered ──→ ASR starts
                │
                ├──→ goroutine: query KB(query) ──→ result → contextQueue
                ├──→ goroutine: recall memory(recall)  ──→ result → contextQueue
                └──→ goroutine: web search(query)  ──→ result → contextQueue
                │
User finishes ──→ ASR ends ──→ startProcessing()
                │
                ├──→ drainContextQueue()  ← non-blockingly fetch arrived results
                ├──→ assemble system prompt + context
                └──→ LLM.StreamChat() ──→ TTS playback
                │
        (goroutines may still be running; results enter the queue and will be drained in the next round)
```

---

## Chapter 2 Voice Agent ↔ Frontend

### 2.1 WebSocket Protocol (Path `/ws`)

After the WebSocket connection is established, text messages (JSON) and binary messages (audio) are transmitted bidirectionally.

#### 2.1.1 Existing Message Types (Unchanged)

**Browser → Server:**


| type | Payload | Description |
| ----------- | ------------------------ | -------------- |
| `vad_start` | None | User starts speaking (VAD triggered) |
| `vad_end` | None | User stops speaking |
| *Binary Frame* | PCM Int16LE, 16kHz, Mono | Real-time audio stream |


**Server → Browser:**


| type | Payload | Description |
| ------------------ | ---------------- | -------------- |
| `status` | `state`: `idle   | listening      |
| `transcript` | `text`: partial recognition result | ASR streaming recognition |
| `transcript_final` | `text`: final recognition result | ASR 2-pass final result |
| `response` | `text`: incremental LLM output | Streaming response |
| *Binary Frame* | MP3/WAV audio data | TTS synthesized audio |


#### 2.1.2 Newly Added Message Types

**Server → Browser (New):**


| type | Payload Fields | Description |
| --------------- | ------------------------------------------------ | -------------- |
| `task_list_update` | `active_task_id`, `tasks` | Task list updated (PPT task created) |
| `task_status` | `task_id`, `status`, `progress` | PPT task status changed |
| `page_rendered` | `task_id`, `page_id`, `render_url`, `page_index` | Single page rendering completed |
| `ppt_preview` | `task_id`, `page_order`, `pages_info[]` | Overall PPT preview data |
| `conflict_ask` | `task_id`, `page_id`, `context_id`, `question` | Display conflict question (paired with voice playback) |
| `export_ready` | `task_id`, `download_url`, `format` | Export file is ready |
| `error` | `code`, `message` | Error notification |


**Browser → Server (New):**


| type | Payload Fields | Description |
| --------------- | ----------------------------------------------------------------- | -------------- |
| `text_input` | `text` | Text input (alternative to speech) |
| `page_navigate` | `task_id`, `page_id` | User switches the page being viewed |
| `task_init` | `topic`, `description`, `total_pages`, `audience`, `global_style` | Initialize a PPT task via UI form |


```go
type WSMessage struct {
    Type      string `json:"type"`
    Text      string `json:"text,omitempty"`
    State     string `json:"state,omitempty"`

    // Task-related
    TaskID    string `json:"task_id,omitempty"`
    Topic     string `json:"topic,omitempty"`
    Status    string `json:"status,omitempty"`
    Progress  int    `json:"progress,omitempty"`       // 0-100

    // Page-related
    PageID    string `json:"page_id,omitempty"`
    PageIndex int    `json:"page_index,omitempty"`
    RenderURL string `json:"render_url,omitempty"`

    // Preview
    PageOrder []string       `json:"page_order,omitempty"`
    PagesInfo []PageInfoBrief `json:"pages_info,omitempty"`

    // Conflict
    ContextID string `json:"context_id,omitempty"`
    Question  string `json:"question,omitempty"`

    // Export
    DownloadURL string `json:"download_url,omitempty"`
    Format      string `json:"format,omitempty"`         // "pptx" | "docx"

    // Form initialization
    Description string `json:"description,omitempty"`
    TotalPages  int    `json:"total_pages,omitempty"`
    Audience    string `json:"audience,omitempty"`
    GlobalStyle string `json:"global_style,omitempty"`

    // Requirements collection
    CollectedFields []string          `json:"collected_fields,omitempty"`
    MissingFields   []string          `json:"missing_fields,omitempty"`
    SummaryText     string            `json:"summary_text,omitempty"`
    Requirements    *TaskRequirements `json:"requirements,omitempty"`
    Confirmed       *bool             `json:"confirmed,omitempty"`
    Modifications   string            `json:"modifications,omitempty"`

    // Error
    Code    int    `json:"code,omitempty"`
    Message string `json:"message,omitempty"`
}

type PageInfoBrief struct {
    PageID     string `json:"page_id"`
    Status     string `json:"status"`
    LastUpdate int64  `json:"last_update"`
    RenderURL  string `json:"render_url"`
}
```

### 2.2 File Upload API

Used by teachers to upload reference materials (PDF, Word, PPT, images, videos).

`**POST /api/v1/upload**`

- Content-Type: `multipart/form-data`
- Authentication: `Authorization: Bearer <token>`

> Routing Note: `/api/v1/upload` is a gateway route exposed by Voice Agent to the frontend. Internally, it forwards to Database Service `POST /api/v1/files/upload`. The frontend only needs to call the route in this section.


| Form Field | Type | Required | Description |
| ------------- | ------ | --- | ------------------------------------------ |
| `file` | File | Yes | Uploaded file |
| `session_id` | string | Yes | Current session ID |
| `task_id` | string | No | Associated PPT task ID (if already created) |
| `purpose` | string | Yes | `reference` (reference material) / `knowledge_base` (to KB) |
| `description` | string | No | File description (teacher's explanation of how the material should be used) |


**Response Body:**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "file_id": "file_aabb1122-...",
    "filename": "Linear_Algebra_Lesson_Plan.pdf",
    "file_type": "pdf",
    "file_size": 2048576,
    "storage_url": "https://oss.example.com/files/file_aabb1122-...",
    "purpose": "reference"
  }
}
```

**File Size and Format Limits:**


| Format | Maximum Size | MIME Types |
| ---- | ------ | --------------------------------------------------------------------------- |
| PDF | 50 MB | `application/pdf` |
| Word | 50 MB | `application/vnd.openxmlformats-officedocument.wordprocessingml.document` |
| PPT | 100 MB | `application/vnd.openxmlformats-officedocument.presentationml.presentation` |
| Image | 20 MB | `image/jpeg`, `image/png`, `image/webp` |
| Video | 500 MB | `video/mp4`, `video/webm` |


### 2.3 PPT Preview API

**`GET /api/v1/tasks/{task_id}/preview`**

- Authentication: `Authorization: Bearer <token>`

**Response Body:**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": "task_abc123",
    "status": "generating",
    "page_order": ["page_uuid1", "page_uuid2", "page_uuid3"],
    "current_viewing_page_id": "page_uuid1",
    "pages": [
      {
        "page_id": "page_uuid1",
        "status": "completed",
        "last_update": 1710680050000,
        "render_url": "https://cdn.example.com/renders/page_uuid1_v3.png"
      },
      {
        "page_id": "page_uuid2",
        "status": "rendering",
        "last_update": 1710680045000,
        "render_url": ""
      }
    ]
  }
}
```

### 2.4 Requirements Elicitation Dialogue Flow

The competition requirements explicitly state: *"It should proactively ask questions to clarify ambiguous requirements, support multi-turn dialogue, and summarize and confirm the final requirements."* Therefore, before starting PPT generation, the Voice Agent must complete one round of **structured requirements elicitation dialogue** with the teacher. Only after all required fields are sufficiently filled may it call `POST /api/v1/ppt/init` to start generation.

#### 2.4.1 Requirements Elicitation Data Structure

At the Session layer, the Voice Agent maintains a `TaskRequirements` struct and fills it incrementally through the conversation:

```go
type TaskRequirements struct {
    SessionID string `json:"session_id"`
    UserID    string `json:"user_id"`

    // ── Required fields (the LLM must keep asking until they are obtained) ──
    Topic           string   `json:"topic"`             // Course topic
    KnowledgePoints []string `json:"knowledge_points"`  // Core knowledge points list
    TeachingGoals   []string `json:"teaching_goals"`    // Teaching goals
    TeachingLogic   string   `json:"teaching_logic"`    // Teaching logic / outline
    TargetAudience  string   `json:"target_audience"`   // Target audience (grade level, expertise level)

    // ── Optional fields (the LLM may proactively ask, but the teacher may skip them) ──
    KeyDifficulties []string `json:"key_difficulties"`  // Key and difficult points
    Duration        string   `json:"duration"`          // Course duration
    TotalPages      int      `json:"total_pages"`       // Expected page count, 0 = agent decides
    GlobalStyle     string   `json:"global_style"`      // Global style (tech style, minimalist style...)
    InteractionDesign string `json:"interaction_design"` // Interaction design ideas (mini-games, animations...)
    OutputFormats   []string `json:"output_formats"`    // Desired outputs ["pptx", "docx", "html5"]
    AdditionalNotes string   `json:"additional_notes"`  // Other supplementary requirements

    // ── Reference materials (obtained in conversation or through upload) ──
    ReferenceFiles  []ReferenceFileReq `json:"reference_files"`

    // ── Metadata ──
    CollectedFields []string `json:"collected_fields"`  // List of already collected field names
    Status          string   `json:"status"`            // collecting | confirming | confirmed | generating
    CreatedAt       int64    `json:"created_at"`
    UpdatedAt       int64    `json:"updated_at"`
}

type ReferenceFileReq struct {
    FileID      string `json:"file_id"`
    FileURL     string `json:"file_url"`
    FileType    string `json:"file_type"`
    Instruction string `json:"instruction"` // What the teacher says: which part of this PDF to refer to, or what format to imitate
}
```

#### 2.4.2 Collection Flow State Machine

```
User: "Help me make a PPT"
    │
    ▼
┌─────────────────────────────────────────────┐
│ Status: collecting                          │
│                                             │
│ LLM system behavior:                        │
│  1. Query Memory Service to prefill known   │
│     preferences                             │
│  2. Ask for missing fields one by one       │
│     according to the checklist              │
│  3. Update TaskRequirements after each turn │
│  4. Allow the teacher to answer multiple    │
│     questions at once                       │
│  5. Allow the teacher to upload reference   │
│     materials and explain how to use them   │
│                                             │
│ Exit condition: all required fields filled  │
└─────────────┬───────────────────────────────┘
              │ All required fields ready
              ▼
┌─────────────────────────────────────────────┐
│ Status: confirming                          │
│                                             │
│ LLM behavior:                               │
│  Generate a structured requirements summary │
│  and read it out to the teacher for         │
│  confirmation                               │
│  "Let me summarize your requirements:       │
│   the topic is... the knowledge points      │
│   include..."                               │
│  "Does this look correct to you? Is there   │
│   anything you'd like to adjust?"           │
│                                             │
│ Branches:                                   │
│  ├─ Teacher says "okay / no problem"        │
│  │   → confirmed                            │
│  └─ Teacher requests changes                │
│      → return to collecting and revise      │
│         fields                              │
└─────────────┬───────────────────────────────┘
              │ Teacher confirms
              ▼
┌─────────────────────────────────────────────┐
│ Status: confirmed → generating              │
│                                             │
│ 1. Assemble TaskRequirements into a         │
│    detailed description                     │
│ 2. Call POST /api/v1/ppt/init               │
│ 3. Push task_created to the frontend via    │
│    WebSocket                                │
│ 4. Voice Agent says "Okay, I'll start       │
│    generating it for you"                   │
└─────────────────────────────────────────────┘
```

#### 2.4.3 LLM Follow-up Checklist

During the requirements elicitation stage, the Voice Agent's LLM uses a dedicated System Prompt with the following checklist embedded. Based on `CollectedFields`, the LLM determines which information is still missing and **proactively but naturally** integrates follow-up questions into the conversation:


| Priority | Field | Example Follow-up Prompt |
| --- | -------------------- | --------------------------------- |
| P0 | `topic` | "What topic would you like this courseware to cover?" |
| P0 | `knowledge_points` | "What are the main knowledge points covered in this lesson?" |
| P0 | `teaching_goals` | "What are the teaching goals for this lesson? To what extent do you want students to master the material?" |
| P0 | `teaching_logic` | "In what order do you plan to teach it? Could you briefly describe your teaching logic?" |
| P0 | `target_audience` | "What level of students is this for? What year are they in? How strong is their academic foundation?" |
| P1 | `key_difficulties` | "Are there any key or difficult points that need special emphasis?" |
| P1 | `duration` | "Roughly how long is this lesson?" |
| P1 | `global_style` | "Do you have any style preferences for the courseware? For example, tech style, minimalist style, academic style?" |
| P1 | `interaction_design` | "Would you like to include any interactive parts, such as quizzes or mini-games?" |
| P2 | `total_pages` | "About how many pages would you like? Or should I decide automatically based on the amount of content?" |
| P2 | `reference_files` | "Do you have any reference materials to upload? For example, previous lesson plans, reference PPTs, or textbook PDFs?" |
| P2 | `additional_notes` | "Are there any other special requirements?" |


**Follow-up Strategy:**

- Ask at most **1-2** questions per turn; do not overwhelm the user all at once
- If the teacher answers multiple fields in one sentence, the LLM should extract them all at once
- User preferences already available in Memory Service (such as `teaching_style`, `visual_preferences`) should be prefilled directly and not asked again
- If the teacher says "you decide" or "anything is fine" for P1/P2 fields, mark them as collected (using default values)
- If the teacher appears impatient or urges the system to proceed, allow skipping the remaining P1/P2 fields and move directly to confirmation

#### 2.4.4 System Prompt Template for the Requirements Elicitation Stage

```
You are a professional teaching assistant helping a teacher design courseware. You need to collect the following information through dialogue in order to produce high-quality PPT courseware.

[Information Already Collected]
{{range .CollectedFields}}
- {{.FieldName}}: {{.Value}}
{{end}}

[Checklist of Information Still To Be Collected]
{{range .MissingFields}}
- [To Be Collected] {{.FieldName}}: {{.Description}}
{{end}}

[User Profile (from memory module)]
{{.UserProfile}}

[Behavior Guidelines]
1. Integrate questions naturally into the conversation; do not ask them mechanically one by one
2. Ask at most 1-2 questions per turn
3. If the teacher provides multiple pieces of information in one sentence, extract them all
4. When the teacher uploads a file, proactively ask: "How would you like me to use this material? Should I reference its content, or imitate its format?"
5. After all required P0 fields are collected, generate a structured summary for the teacher to confirm
6. After confirmation, output the special marker: [REQUIREMENTS_CONFIRMED]
```

#### 2.4.5 Mapping TaskRequirements → PPT Init Request

After the teacher confirms the requirements, the Voice Agent assembles `TaskRequirements` into `PPTInitRequest`:

```go
func (r *TaskRequirements) ToPPTInitRequest() PPTInitRequest {
    description := buildDetailedDescription(r)
    return PPTInitRequest{
        UserID:         r.UserID,
        Topic:          r.Topic,
        Description:    description,          // Detailed description assembled from all collected fields
        TotalPages:     r.TotalPages,
        Audience:       r.TargetAudience,
        GlobalStyle:    r.GlobalStyle,
        SessionID:      r.SessionID,
        TeachingElements: &InitTeachingElements{
            KnowledgePoints:   r.KnowledgePoints,
            TeachingGoals:     r.TeachingGoals,
            TeachingLogic:     r.TeachingLogic,
            KeyDifficulties:   r.KeyDifficulties,
            Duration:          r.Duration,
            InteractionDesign: r.InteractionDesign,
            OutputFormats:     r.OutputFormats,
        },
        ReferenceFiles: toReferenceFiles(r.ReferenceFiles),
    }
}

func toReferenceFiles(in []ReferenceFileReq) []ReferenceFile {
    out := make([]ReferenceFile, 0, len(in))
    for _, f := range in {
        out = append(out, ReferenceFile{
            FileID:      f.FileID,
            FileURL:     f.FileURL,
            FileType:    f.FileType,
            Instruction: f.Instruction,
        })
    }
    return out
}

// buildDetailedDescription assembles all collected structured information into
// a detailed natural-language description for the PPT Agent's LLM to understand and execute.
func buildDetailedDescription(r *TaskRequirements) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf("[Course Topic] %s\n", r.Topic))
    sb.WriteString(fmt.Sprintf("[Teaching Goals] %s\n", strings.Join(r.TeachingGoals, "; ")))
    sb.WriteString(fmt.Sprintf("[Core Knowledge Points] %s\n", strings.Join(r.KnowledgePoints, ", ")))
    sb.WriteString(fmt.Sprintf("[Teaching Logic] %s\n", r.TeachingLogic))
    sb.WriteString(fmt.Sprintf("[Target Audience] %s\n", r.TargetAudience))
    if len(r.KeyDifficulties) > 0 {
        sb.WriteString(fmt.Sprintf("[Key and Difficult Points] %s\n", strings.Join(r.KeyDifficulties, ", ")))
    }
    if r.Duration != "" {
        sb.WriteString(fmt.Sprintf("[Course Duration] %s\n", r.Duration))
    }
    if r.InteractionDesign != "" {
        sb.WriteString(fmt.Sprintf("[Interaction Design] %s\n", r.InteractionDesign))
    }
    if r.AdditionalNotes != "" {
        sb.WriteString(fmt.Sprintf("[Other Requirements] %s\n", r.AdditionalNotes))
    }
    return sb.String()
}
```

#### 2.4.6 Newly Added Frontend WebSocket Message Types


| Direction | type | Payload Fields | Description |
| --- | ----------------------- | --------------------------------------------------------- | ---------- |
| S→C | `requirements_progress` | `collected_fields[]`, `missing_fields[]`, `status` | Display real-time requirements collection progress |
| S→C | `requirements_summary` | `summary_text`, `requirements` (complete TaskRequirements JSON) | Display the requirements summary for confirmation |
| C→S | `requirements_confirm` | `confirmed`: bool, `modifications`: string | Teacher confirms or requests modifications |


#### 2.4.7 Integration with Memory Service

At the start of requirements collection, the Voice Agent immediately queries the Memory Service to prefill known information:

```go
func (p *Pipeline) prefillFromMemory(ctx context.Context, req *TaskRequirements) {
    profile, err := memClient.GetProfile(req.UserID)
    if err != nil { return }

    if profile.Subject != "" && req.TargetAudience == "" {
        req.TargetAudience = profile.Subject + " major students"
        req.CollectedFields = append(req.CollectedFields, "target_audience")
    }
    if style, ok := profile.VisualPreferences["color_scheme"]; ok && req.GlobalStyle == "" {
        req.GlobalStyle = style
        req.CollectedFields = append(req.CollectedFields, "global_style")
    }
}
```

After requirements collection is completed, asynchronously write newly collected preferences from this session into Memory Service:

```go
go func() {
    memClient.Extract(MemoryExtractRequest{
        UserID:    req.UserID,
        SessionID: req.SessionID,
        Messages:  requirementCollectionDialogue,
    })
}()
```

---

## Chapter 3 Voice Agent ↔ PPT Agent

This chapter reuses the interfaces already defined in the Data Interface document and supplements the missing parts. PPT Agent listens on port `9100` by default.

### 3.1 Underlying State Model (Redis)

#### 3.1.1 VAD Ultra-Fast Trigger Signal

At the first millisecond when the user starts speaking, the Voice Agent generates a signal that triggers Redis to perform an instant deep copy of the canvas snapshot.

```go
type VADEvent struct {
    TaskID        string `json:"task_id"`
    Timestamp     int64  `json:"timestamp"`        // T_vad: Unix ms
    ViewingPageID string `json:"viewing_page_id"`  // Page ID displayed on screen when the user starts speaking
}
```

#### 3.1.2 Global Canvas Snapshot Tree

Redis Key: `snapshot:{task_id}:{timestamp}`, TTL = 300 seconds

```go
type CanvasSnapshot struct {
    TaskID    string              `json:"task_id"`
    Timestamp int64               `json:"timestamp"`
    PageOrder []string            `json:"page_order"` // Routing table maintaining rendering order
    Pages     map[string]PageCode `json:"pages"`      // Key = PageID
}

type PageCode struct {
    PageID string `json:"page_id"`
    PyCode string `json:"py_code"`
    Status string `json:"status"` // rendering | completed | failed | suspended_for_human
}
```

### 3.2 Task Initialization

`**POST /api/v1/ppt/init**` - Create a PPT generation task

**Request Body:**

```go
type PPTInitRequest struct {
    UserID      string `json:"user_id"`
    Topic       string `json:"topic"`               // Required
    Description string `json:"description"`         // Required: detailed description assembled during requirements collection (see 2.4.5)
    TotalPages  int    `json:"total_pages"`         // Expected page count; 0 means the Agent decides automatically
    Audience    string `json:"audience"`            // Target audience
    GlobalStyle string `json:"global_style"`        // Global style
    SessionID   string `json:"session_id"`          // Associated session ID

    // Structured teaching elements - extracted during requirements collection and used by PPT Agent for precise generation
    TeachingElements *InitTeachingElements `json:"teaching_elements,omitempty"`

    ReferenceFiles []ReferenceFile `json:"reference_files,omitempty"`
}

type InitTeachingElements struct {
    KnowledgePoints []string `json:"knowledge_points"` // Core knowledge points list
    TeachingGoals   []string `json:"teaching_goals"`   // Teaching goals
    TeachingLogic   string   `json:"teaching_logic"`   // Teaching logic/outline
    KeyDifficulties []string `json:"key_difficulties"` // Key and difficult points
    Duration        string   `json:"duration"`         // Course duration
    InteractionDesign string `json:"interaction_design"` // Interaction design ideas
    OutputFormats   []string `json:"output_formats"`   // Desired output formats
}

type ReferenceFile struct {
    FileID      string `json:"file_id"`
    FileURL     string `json:"file_url"`
    FileType    string `json:"file_type"`   // pdf | docx | pptx | image | video
    Instruction string `json:"instruction"` // Teacher's instructions on how to use this material (e.g. "imitate the content format of Chapter 3 in this PDF")
}
```

**Response Body:**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": "task_abc123"
  }
}
```

**Error Codes:**


| code | Description |
| ----- | ---------------------- |
| 40001 | `topic` or `description` is empty |
| 40400 | `user_id` does not exist |
| 50000 | Internal error |


### 3.3 Structured Feedback and Intent Dispatch

**`POST /api/v1/ppt/feedback`** - Voice Agent submits user feedback to PPT Agent

**Request Body:**

```go
type PPTFeedbackRequest struct {
    TaskID            string   `json:"task_id"`
    BaseTimestamp     int64    `json:"base_timestamp"`       // Corresponds to T_vad in VADEvent
    ViewingPageID     string   `json:"viewing_page_id"`      // Page viewed when the user started speaking
    ReplyToContextID  string   `json:"reply_to_context_id"`  // If replying to an Agent question, fill in the corresponding context_id; otherwise leave empty
    RawText           string   `json:"raw_text"`             // Raw ASR text
    Intents           []Intent `json:"intents"`
}

type Intent struct {
    ActionType   string `json:"action_type"`    // modify | insert_before | insert_after | delete | global_modify | resolve_conflict
    TargetPageID string `json:"target_page_id"` // Target page ID; for global_modify fill in "ALL"
    Instruction  string `json:"instruction"`    // Natural-language modification instruction
}
```

**ActionType Enum:**


| Value | Semantics |
| ------------------ | ----------------------------------------- |
| `modify` | Modify the content of the specified page |
| `insert_before` | Insert a new page before the specified page |
| `insert_after` | Insert a new page after the specified page |
| `delete` | Delete the specified page |
| `global_modify` | Global modification (e.g. change background color for all pages), `target_page_id` = `"ALL"` |
| `resolve_conflict` | Respond to a previous conflict question; must include `reply_to_context_id` |


**Response Body:**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "accepted_intents": 2,
    "queued": true
  }
}
```

**Error Codes:**


| code | Description |
| ----- | ------------ |
| 40001 | `intents` array is empty |
| 40400 | `task_id` does not exist |
| 40900 | Task has already terminated and no longer accepts feedback |


### 3.4 Reverse Assistance API (PPT Agent → Voice Agent)

**`POST /api/v1/voice/ppt_message`** - PPT Agent calls Voice Agent to speak an assistance message

This is proactively called by PPT Agent when conflict resolution returns `ask_human`. After receiving it, Voice Agent triggers immediate TTS playback through `highPriorityQueue`.

Compatibility Note: To remain compatible with the existing Data Interface document, it is recommended to keep the alias route `POST /api/v1/voice/ppt_message_tool` as well (behavior exactly the same as this interface).

**Request Body:**

```go
type PPTMessageRequest struct {
    TaskID    string `json:"task_id"`
    PageID    string `json:"page_id"`
    Priority  string `json:"priority"`     // "high" (conflict assistance request) | "normal" (status notification)
    ContextID string `json:"context_id"`   // Context clue ID; after the user replies it must be returned unchanged
    TTSText   string `json:"tts_text"`     // Text to be spoken
    MsgType   string `json:"msg_type"`     // "conflict_question" | "ppt_status"
}
```

**Response Body:**

```json
{
  "code": 200,
  "message": "accepted"
}
```

Voice Agent handling logic:

1. Convert the request into a `ContextMessage` and place it into the appropriate queue based on `Priority`
2. If `priority == "high"` and `msg_type == "conflict_question"`, immediately trigger TTS playback for `tts_text`
3. Record `context_id` + `page_id` into the Session's `pendingQuestions` list
4. On the user's next speech input, the LLM determines whether the user is answering this question; if so, construct a `resolve_conflict` intent and send it back to PPT Agent together with `context_id`

### 3.5 Canvas State Query

`**GET /api/v1/canvas/status?task_id={task_id}**`

**Response Body:**

```go
type CanvasStatusResponse struct {
    TaskID              string          `json:"task_id"`
    PageOrder           []string        `json:"page_order"`
    CurrentViewingPageID string         `json:"current_viewing_page_id"`
    PagesInfo           []PageStatusInfo `json:"pages_info"`
}

type PageStatusInfo struct {
    PageID     string `json:"page_id"`
    Status     string `json:"status"`      // rendering | completed | failed | suspended_for_human
    LastUpdate int64  `json:"last_update"` // Unix ms
    RenderURL  string `json:"render_url"`  // URL of the rendered image after completion
}
```

### 3.6 PPT Export (New)

`**POST /api/v1/ppt/export**` - Export PPT as .pptx / .docx

**Request Body:**

```go
type PPTExportRequest struct {
    TaskID string `json:"task_id"`
    Format string `json:"format"` // "pptx" | "docx" | "html5"
}
```

**Response Body:**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "export_id": "file_export001",
    "status": "generating",
    "estimated_seconds": 30
  }
}
```

After export is complete, PPT Agent notifies Voice Agent through `POST /api/v1/voice/ppt_message` (`msg_type = "ppt_status"`), and Voice Agent then pushes an `export_ready` WebSocket message to the frontend.

**Export Status Query:**

**`GET /api/v1/ppt/export/{export_id}`**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "export_id": "file_export001",
    "status": "completed",
    "download_url": "https://oss.example.com/exports/task_abc123.pptx",
    "format": "pptx",
    "file_size": 5242880
  }
}
```

### 3.7 Single-Page Rendering Result Query (New)

`**GET /api/v1/ppt/page/{page_id}/render?task_id={task_id}**`

```go
type PageRenderResponse struct {
    PageID    string `json:"page_id"`
    TaskID    string `json:"task_id"`
    Status    string `json:"status"`
    RenderURL string `json:"render_url"`
    PyCode    string `json:"py_code"`   // Python source code of the current page
    Version   int    `json:"version"`   // Rendering version number
    UpdatedAt int64  `json:"updated_at"`
}
```

### 3.8 Internal Scheduling Structures (PPT Agent Implementation Reference)

The following structs are for internal use within PPT Agent and are not externally visible, but are defined here to ensure shared understanding across the team:

#### 3.8.1 Three-Way Merge Payload

```go
type ThreeWayMergeTask struct {
    TaskID      string `json:"task_id"`
    PageID      string `json:"page_id"`
    CurrentCode string `json:"current_code"` // V_current: latest iterated code in the system
    SystemPatch string `json:"system_patch"` // Diff from V_base → V_current
    Instruction string `json:"instruction"`  // User modification instruction (natural language)
}
```

#### 3.8.2 LLM Conflict Resolution Output

```go
type MergeResult struct {
    PageID          string `json:"page_id"`
    MergeStatus     string `json:"merge_status"`      // auto_resolved | ask_human
    MergedPyCode    string `json:"merged_pycode"`      // Present only when auto_resolved
    QuestionForUser string `json:"question_for_user"`  // Present only when ask_human
}
```

#### 3.8.3 Rendering Executor

```go
type RenderJob struct {
    TaskID string
    PageID string
    PyCode string
}

type RenderResponse struct {
    Success bool
    Error   string
}

type CanvasRenderer interface {
    Execute(ctx context.Context, job RenderJob) RenderResponse
}
```

### 3.9 Timing Constraints (Mandatory Rules)

#### 3.9.1 Suspended Page Handling

When the status of a page is `suspended_for_human`:

1. **Irrelevant feedback**: the user submits new feedback for that page, but it is unrelated to the suspension reason → temporarily store the new feedback in that page's `pendingFeedbacks` queue, **do not lift the suspension**, and resend the suspension-reason assistance message to Voice Agent (`priority=high`)
2. **Timeout re-ask**: if the page remains suspended for more than **45 seconds** without a response → send the assistance message to Voice Agent again
3. **Timeout self-decision**: if the page remains suspended for more than **3 minutes** → let the LLM decide on its own (usually choosing the system-optimized version), lift the suspension, and process `pendingFeedbacks`
4. **Correct response**: if a `resolve_conflict` intent carrying the correct `context_id` is received → lift the suspension, let the LLM regenerate the code based on the user's answer, and then process `pendingFeedbacks`

```go
type SuspendedPage struct {
    PageID           string    `json:"page_id"`
    ContextID        string    `json:"context_id"`
    Reason           string    `json:"reason"`
    SuspendedAt      int64     `json:"suspended_at"`
    LastAskedAt      int64     `json:"last_asked_at"`
    AskCount         int       `json:"ask_count"`
    PendingFeedbacks []Intent  `json:"pending_feedbacks"`
}
```

#### 3.9.2 Concurrent Feedback Queueing

When a three-way merge is still running and new feedback for the same page arrives:

1. **Do not interrupt the current merge**; append the new feedback to that page's `pendingFeedbacks` queue
2. After the current merge completes, take the next feedback from the queue and start a new round of merge
3. For the new round of merge, use the previous round's merge result as `V_base`, and fetch the latest `V_current` from the Canvas

```go
type PageMergeState struct {
    PageID           string
    IsRunning        bool
    CurrentCtx       context.Context
    CurrentCancel    context.CancelFunc
    PendingFeedbacks []Intent
    Mu               sync.Mutex
}
```

#### 3.9.3 Three Conflict Resolution Paths


| Condition | Handling |
| ------------- | ---------------------------- |
| No code conflict + no logic conflict | Merge directly, without using the LLM |
| Code conflict + no logic conflict | Perform three-way merge; LLM returns `auto_resolved` |
| Code conflict + logic conflict | Perform three-way merge; LLM returns `ask_human`, and the page is suspended |


---

## Chapter 4 Knowledge Base Service

This module implements the core RAG (Retrieval-Augmented Generation) pipeline: document ingestion → chunking → vectorization → storage → semantic retrieval. Default port: `9200`.

### 4.1 Technology Stack Conventions


| Component | Selection |
| ------------ | ------------------------------- |
| Vector Database | Milvus or Qdrant (choose one; unified interface) |
| Embedding Model | BAAI/bge-m3 or a Chinese embedding model of equivalent level |
| Document Parsing | See the unified pipeline in Chapter 8 |
| HTTP Framework | Go (Gin / Chi) |


### 4.2 Data Models

#### 4.2.1 KB Collection

```go
type KBCollection struct {
    CollectionID string `json:"collection_id"` // coll_<uuid>
    UserID       string `json:"user_id"`
    Name         string `json:"name"`
    Subject      string `json:"subject"`       // Subject: mathematics, physics, computer science...
    Description  string `json:"description"`
    DocCount     int    `json:"doc_count"`
    CreatedAt    int64  `json:"created_at"`
    UpdatedAt    int64  `json:"updated_at"`
}
```

#### 4.2.2 KB Document

```go
type KBDocument struct {
    DocID        string `json:"doc_id"`         // doc_<uuid>
    CollectionID string `json:"collection_id"`
    FileID       string `json:"file_id"`        // Associated file ID from Database Service
    Title        string `json:"title"`
    DocType      string `json:"doc_type"`       // pdf | docx | pptx | image | video | text
    ChunkCount   int    `json:"chunk_count"`
    Status       string `json:"status"`         // pending | processing | indexed | failed
    ErrorMessage string `json:"error_message,omitempty"`
    CreatedAt    int64  `json:"created_at"`
}
```

#### 4.2.3 Text Chunk

```go
type TextChunk struct {
    ChunkID  string    `json:"chunk_id"`  // chunk_<uuid>
    DocID    string    `json:"doc_id"`
    Content  string    `json:"content"`   // Text content
    Metadata ChunkMeta `json:"metadata"`
}

type ChunkMeta struct {
    PageNumber  int    `json:"page_number,omitempty"`  // PDF/PPT page number
    SectionTitle string `json:"section_title,omitempty"` // Section title
    ChunkIndex  int    `json:"chunk_index"`             // Sequence number within the document
    StartChar   int    `json:"start_char"`              // Starting character position in the original text
    EndChar     int    `json:"end_char"`
    ImageURL    string `json:"image_url,omitempty"`     // Associated image URL
    SourceType  string `json:"source_type"`             // text | ocr | video_transcript
}
```

### 4.3 HTTP API

#### 4.3.1 Create KB Collection

`**POST /api/v1/kb/collections**`

**Request Body:**

```go
type CreateCollectionRequest struct {
    UserID      string `json:"user_id"`
    Name        string `json:"name"`        // Required
    Subject     string `json:"subject"`     // Required
    Description string `json:"description"`
}
```

**Response:**

```json
{
  "code": 200,
  "data": {
    "collection_id": "coll_aabbccdd-..."
  }
}
```

#### 4.3.2 List KB Collections

`**GET /api/v1/kb/collections?user_id={user_id}**`

**Response:**

```json
{
  "code": 200,
  "data": {
    "collections": [
      {
        "collection_id": "coll_aabb",
        "name": "University Mathematics",
        "subject": "Mathematics",
        "doc_count": 12,
        "created_at": 1710000000000
      }
    ],
    "total": 1
  }
}
```

#### 4.3.3 Upload and Index Document

`**POST /api/v1/kb/documents**`

This interface receives file metadata and triggers the asynchronous indexing process. The actual file has already been uploaded to object storage through `POST /api/v1/upload`.

**Request Body:**

```go
type IndexDocumentRequest struct {
    CollectionID string `json:"collection_id"` // Required
    FileID       string `json:"file_id"`       // Required: file ID in Database Service
    FileURL      string `json:"file_url"`      // Required: object storage URL
    FileType     string `json:"file_type"`     // Required: pdf | docx | pptx | image | video | text
    Title        string `json:"title"`
}
```

**Response:**

```json
{
  "code": 200,
  "data": {
    "doc_id": "doc_55667788-...",
    "status": "processing"
  }
}
```

Indexing is asynchronous. The caller may query indexing progress through `GET /api/v1/kb/documents/{doc_id}`.

#### 4.3.4 Query Document Index Status

`**GET /api/v1/kb/documents/{doc_id}**`

**Response:**

```json
{
  "code": 200,
  "data": {
    "doc_id": "doc_55667788",
    "collection_id": "coll_aabb",
    "title": "Linear Algebra Lecture Notes",
    "doc_type": "pdf",
    "chunk_count": 45,
    "status": "indexed",
    "created_at": 1710000000000
  }
}
```

#### 4.3.5 Semantic Retrieval

`**POST /api/v1/kb/query**` - Core interface, asynchronously called by Voice Agent

**Request Body:**

```go
type KBQueryRequest struct {
    CollectionID string            `json:"collection_id"`        // Optional: if empty, search across all collections of the user
    UserID       string            `json:"user_id"`              // Required
    Query        string            `json:"query"`                // Required: natural-language query
    TopK         int               `json:"top_k"`                // Number of returned results, default 5, max 20
    ScoreThreshold float64         `json:"score_threshold"`      // Minimum similarity threshold, default 0.5
    Filters      map[string]string `json:"filters,omitempty"`    // Metadata filters
}
```

**Response Body:**

```go
type KBQueryResponse struct {
    Chunks []RetrievedChunk `json:"chunks"`
    Total  int              `json:"total"`
}

type RetrievedChunk struct {
    ChunkID      string    `json:"chunk_id"`
    DocID        string    `json:"doc_id"`
    DocTitle     string    `json:"doc_title"`
    Content      string    `json:"content"`
    Score        float64   `json:"score"`        // Similarity score 0-1
    Metadata     ChunkMeta `json:"metadata"`
}
```

**Error Codes:**


| code | Description |
| ----- | ------------------ |
| 40001 | `query` or `user_id` is empty |
| 50200 | Vector database unavailable |


#### 4.3.6 Delete Document

**`DELETE /api/v1/kb/documents/{doc_id}`**

Cascade deletion: document metadata + all chunks in the vector database.

**Response:**

```json
{ "code": 200, "message": "deleted" }
```

#### 4.3.7 List Documents in a Collection

`**GET /api/v1/kb/collections/{collection_id}/documents?page=1&page_size=20**`

**Response:**

```json
{
  "code": 200,
  "data": {
    "documents": [ ... ],
    "total": 12,
    "page": 1,
    "page_size": 20
  }
}
```

#### 4.3.8 Bulk Ingestion of Search Results into KB

`**POST /api/v1/kb/ingest-from-search**`

When Web Search finds valuable content, it asynchronously calls this interface to persist the results into the user's knowledge base for future reuse through RAG.

**Request Body:**

```go
type IngestFromSearchRequest struct {
    UserID       string               `json:"user_id"`
    CollectionID string               `json:"collection_id,omitempty"` // If empty, automatically place into the user's default collection
    Items        []SearchIngestItem   `json:"items"`
}

type SearchIngestItem struct {
    Title   string `json:"title"`
    URL     string `json:"url"`
    Content string `json:"content"`   // Refined main content (not raw HTML)
    Source  string `json:"source"`    // Source website
}
```

**Response:**

```json
{
  "code": 200,
  "data": {
    "ingested": 3,
    "skipped":  1,
    "doc_ids": ["doc_aabb1122", "doc_ccdd3344", "doc_eeff5566"]
  }
}
```

**Processing Logic:**

1. Deduplicate by URL - if the URL already exists in the user's knowledge base, skip it (`skipped`)
2. Create one lightweight document record for each item (`doc_type: "web_snippet"`)
3. Chunking + vectorization follow the same asynchronous indexing workflow as regular documents
4. Mark metadata with `origin: "web_search"` for later distinction between manual uploads and automatic persistence

**Error Codes:**

| code | Description |
| ----- | -------------- |
| 40001 | `user_id` is empty |
| 40002 | `items` is empty |
| 50200 | Vector database unavailable |

### 4.4 Voice Agent Calling Pattern

Voice Agent asynchronously initiates a KB query when the user starts speaking (VAD triggered):

```go
// In pipeline.StartListening(), after receiving the first batch of ASR results
p.asyncQuery(ctx, "knowledge_base", func() (string, error) {
    resp, err := kbClient.Query(KBQueryRequest{
        UserID: session.UserID,
        Query:  partialASRText,
        TopK:   5,
    })
    if err != nil { return "", err }
    return formatChunksForLLM(resp.Chunks), nil
})
```

---

## Chapter 5 Memory Service

This module manages the user's short-term memory (working memory) and long-term memory (facts + preferences), providing personalized context for Voice Agent. Default port: `9300`.

### 5.1 Memory System Design

Referencing the three-layer memory capability in Berger's article:


| Layer | Name | Storage | TTL | Description |
| --- | ---- | ---------- | ------------------- | ----------------------- |
| L1 | Working Memory | Redis | Session duration (default 4h) | Dialogue summary and extracted teaching elements for the current session |
| L2 | Factual Memory | PostgreSQL | Permanent | User's objective facts: name, school, teaching subject, frequently used textbook |
| L3 | Preference Memory | PostgreSQL | Permanent (with confidence decay) | User's subjective preferences: teaching style, color preference, content depth |


### 5.2 Data Models

#### 5.2.1 Working Memory

Redis Key: `working_mem:{session_id}`, Value = JSON

```go
type WorkingMemory struct {
    SessionID       string              `json:"session_id"`
    UserID          string              `json:"user_id"`
    ConversationSummary string          `json:"conversation_summary"` // LLM-generated dialogue summary
    ExtractedElements TeachingElements  `json:"extracted_elements"`   // Extracted teaching elements
    RecentTopics    []string            `json:"recent_topics"`
    UpdatedAt       int64               `json:"updated_at"`
}

type TeachingElements struct {
    KnowledgePoints []string `json:"knowledge_points"` // Knowledge points list
    TeachingGoals   []string `json:"teaching_goals"`   // Teaching goals
    KeyDifficulties []string `json:"key_difficulties"` // Key and difficult points
    TargetAudience  string   `json:"target_audience"`  // Target audience
    Duration        string   `json:"duration"`         // Course duration
    OutputStyle     string   `json:"output_style"`     // Output style
}
```

#### 5.2.2 Long-Term Memory Entry

```go
type MemoryEntry struct {
    MemoryID   string  `json:"memory_id"`    // mem_<uuid>
    UserID     string  `json:"user_id"`
    Category   string  `json:"category"`     // fact | preference | summary
    Key        string  `json:"key"`          // e.g. "name", "teaching_style", "color_preference"
    Value      string  `json:"value"`        // Value
    Context    string  `json:"context"`      // Context where this memory applies (e.g. "mathematics courseware", "general")
    Confidence float64 `json:"confidence"`   // Confidence 0-1; preference items decay over time
    Source     string  `json:"source"`       // Source of memory: explicit (explicitly stated by user) | inferred
    SourceSessionID string `json:"source_session_id,omitempty"`
    CreatedAt  int64   `json:"created_at"`
    UpdatedAt  int64   `json:"updated_at"`
}
```

#### 5.2.3 User Profile (Aggregated View)

```go
type UserProfile struct {
    UserID          string            `json:"user_id"`
    DisplayName     string            `json:"display_name"`
    Subject         string            `json:"subject"`           // Teaching subject
    School          string            `json:"school"`
    TeachingStyle   string            `json:"teaching_style"`    // rigorous / interactive / narrative ...
    ContentDepth    string            `json:"content_depth"`     // basic / intermediate / advanced
    VisualPreferences map[string]string `json:"visual_preferences"` // color / font / layout preferences
    Preferences     map[string]string `json:"preferences"`       // Other preference KVs
    HistorySummary  string            `json:"history_summary"`   // Summary of historical interactions
    LastActiveAt    int64             `json:"last_active_at"`
}
```

### 5.3 HTTP API

#### 5.3.1 Extract Memory (from Dialogue)

`**POST /api/v1/memory/extract**` - Internal LLM analyzes dialogue to extract facts and preferences

**Request Body:**

```go
type MemoryExtractRequest struct {
    UserID       string   `json:"user_id"`
    SessionID    string   `json:"session_id"`
    Messages     []ConversationTurn `json:"messages"` // Most recent N turns of dialogue
}

type ConversationTurn struct {
    Role    string `json:"role"`    // user | assistant
    Content string `json:"content"`
}
```

```json
{
  "user_id": "string",           // Required
  "session_id": "string",        // Required
  "messages": [                  // Required: dialog lists
    {
      "role": "string",          // Required: "user", "assistant"
      "content": "string"        // Required
    }
  ]
}
```

**Response Body:**

```go
type MemoryExtractResponse struct {
    ExtractedFacts       []MemoryEntry `json:"extracted_facts"`
    ExtractedPreferences []MemoryEntry `json:"extracted_preferences"`
    ConversationSummary  string        `json:"conversation_summary"`
}
```

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "extracted_facts": ["string"],        // optional
    "extracted_preferences": ["string"],  // optional
    "conversation_summary": "string"      // optional
  }
}
```

**using example**:

```bash
curl -X POST http://memory-service-url/api/v1/memory/extract \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_abc123",
    "messages": [
      {"role": "user", "content": "我喜欢简洁的PPT风格"},
      {"role": "assistant", "content": "好的，我会为您制作简洁风格的课件"}
    ]
  }'
```

---

Memory Service internally calls the LLM to perform extraction. Example prompt:

```
System Prompt: Extract the user's factual information and preference information from the following dialogue, and output it in JSON format.
Facts: objective information explicitly stated by the user (name, school, subject, etc.)
Preferences: subjective inclinations expressed by the user (style preference, content preference, etc.), with confidence annotated
```

#### 5.3.2 Recall Memory

`**POST /api/v1/memory/recall**` - Asynchronously called by Voice Agent to obtain memory relevant to the current query

**Request Body:**

```go
type MemoryRecallRequest struct {
    UserID    string `json:"user_id"`    // Required
    SessionID string `json:"session_id"` // Optional: if empty, working memory is not included
    Query     string `json:"query"`      // Required: current user utterance
    TopK      int    `json:"top_k"`      // Required: Number of returned results, default 10
}
```

```json
{
  "user_id": "string",           // Required
  "session_id": "string",        // optional
  "query": "string",             // Required
  "top_k": 0                     // Required: results, int, >0
}
```

**Response Body:**

```go
type MemoryRecallResponse struct {
    Facts           []MemoryEntry   `json:"facts"`            // Relevant facts
    Preferences     []MemoryEntry   `json:"preferences"`      // Relevant preferences
    WorkingMemory   *WorkingMemory  `json:"working_memory"`   // Working memory (if session_id is provided)
    ProfileSummary  string          `json:"profile_summary"`  // User profile summary text
}
```

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "facts": [                   // Required，facts lists
      {
        "key": "string",         // optional
        "content": "string",     // optional
        "value": "string",       // optional
        "confidence": 0.0        // optional: float 0.0-1.0
      }
    ],
    "preferences": [             // Required, perferences lists
      {
        "key": "string",         // optional
        "content": "string",     // optional
        "value": "string",       // optional
        "confidence": 0.0        // optional: float 0.0-1.0
      }
    ],
    "working_memory": {          // optional
      "session_id": "string",    // optional
      "user_id": "string",       // optional
      "conversation_summary": "string",     // optional
      "extracted_elements": {},             // optional: (any json object)
      "recent_topics": ["string"],          // optional
      "metadata": {                         // optional
        "key": "value"
      }
    },
    "profile_summary": "string"  // optional
  }
}
```

**Using example**:

```bash
curl -X POST http://memory-service-url/api/v1/memory/recall \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_abc123",
    "query": "Teaching style of user",
    "top_k": 10
  }'
```

---

#### 5.3.3 Get User Profile

`**GET /api/v1/memory/profile/{user_id}**`

**Response:**

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "user_id": "string",                  // Required
    "display_name": "string",             // optional
    "subject": "string",                  // optional
    "school": "string",                   // optional
    "teaching_style": "string",           // optional
    "content_depth": "string",            // optional
    "preferences": {                      // optional:(key->value)
      "key": "value"
    },
    "visual_preferences": {               // optional:(key->value)
      "key": "value"
    },
    "history_summary": "string",          // optional
    "last_active_at": 0                   // optional: ms
  }
}
```

**using example**:

```bash
curl -X GET http://memory-service-url/api/v1/memory/profile/user_001
```

---

#### 5.3.4 Update User Profile

`**PUT /api/v1/memory/profile/{user_id}**`

**Request Body:** Pass only the fields that need to be updated (partial update)

```go
type UpdateProfileRequest struct {
    DisplayName     string            `json:"display_name,omitempty"`
    Subject         string            `json:"subject,omitempty"`
    TeachingStyle   string            `json:"teaching_style,omitempty"`
    VisualPreferences map[string]string `json:"visual_preferences,omitempty"`
    Preferences     map[string]string `json:"preferences,omitempty"`
}
```

#### 5.3.5 Save Working Memory

`**POST /api/v1/memory/working/save**`

**Request Body:**

```go
type SaveWorkingMemoryRequest struct {
    SessionID           string           `json:"session_id"`
    UserID              string           `json:"user_id"`
    ConversationSummary string           `json:"conversation_summary"`
    ExtractedElements   TeachingElements `json:"extracted_elements"`
    RecentTopics        []string         `json:"recent_topics"`
}
```

```json
{
  "session_id": "string",        // Required
  "user_id": "string",           // Required
  "conversation_summary": "string",     // Optional
  "extracted_elements": {},             // Optional: (any json object)
  "recent_topics": ["string"]           // Optional
}
```

**Response**:

```json
{
  "code": 200,
  "message": "success"
}
```

**using example**:

```bash
curl -X POST http://memory-service-url/api/v1/memory/working/save \
  -H "Content-Type: application/json" \
  -d '{
    "session_id": "sess_abc123",
    "user_id": "user_001",
    "conversation_summary": "用户正在制作高等数学课件",
    "extracted_elements": {"topic": "导数与微分"},
    "recent_topics": ["导数", "微分"]
  }'
```

---

#### 5.3.6 Get Working Memory

`**GET /api/v1/memory/working/{session_id}**`

**Request Body:**:

- `session_id` (path parameter): Required

**Response**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "session_id": "string",               // Optional
    "user_id": "string",                  // Optional
    "conversation_summary": "string",     // Optional
    "extracted_elements": {},             // Optional: (any json object)
    "recent_topics": ["string"],          // Optional
    "metadata": {"key": "value"}          // Optional
  }
}
```

**使用示例**:

```bash
curl -X GET http://memory-service-url/api/v1/memory/working/sess_abc123
```

---

### 5.4 Preference Confidence Decay Mechanism

The `confidence` of preference memories decays naturally over time to prevent outdated preferences from affecting current decisions:

- Preferences reconfirmed each time the user is active: reset `confidence` to the value at extraction time
- Preferences not reconfirmed for more than 30 days: `confidence *= 0.9` (10% decay every 30 days)
- Preferences with `confidence < 0.3`: marked as `[Low Confidence]` during recall, and the LLM may ignore them as appropriate

### 5.5 Voice Agent Calling Pattern

```go
// Asynchronously query memory immediately after VAD is triggered
p.asyncQuery(ctx, "memory", func() (string, error) {
    resp, err := memClient.Recall(MemoryRecallRequest{
        UserID:    session.UserID,
        SessionID: session.SessionID,
        Query:     partialASRText,
        TopK:      10,
    })
    if err != nil { return "", err }
    return formatMemoryForLLM(resp), nil
})

// Asynchronously extract memory after each dialogue round (does not affect response)
go func() {
    memClient.Extract(MemoryExtractRequest{
        UserID:    session.UserID,
        SessionID: session.SessionID,
        Messages:  last5Turns,
    })
}()
```

---

## Chapter 6 Web Search Service

This module provides Voice Agent with real-time web search capability to supplement the latest information not covered by the knowledge base. Default port: `9400`.

### 6.1 Design Principles

1. **Never block the Voice Agent**: Voice Agent calls it asynchronously through a goroutine, and the result is injected back via `ContextMessage`
2. **Result enhancement**: before returning search results, optionally cross-deduplicate them with the Knowledge Base (to avoid injecting information already present in the KB)
3. **Result refinement**: use the LLM to summarize and refine search results instead of directly returning raw webpage content

### 6.2 Data Models

```go
type SearchRequest struct {
    RequestID  string `json:"request_id"`   // search_<uuid>
    UserID     string `json:"user_id"`
    Query      string `json:"query"`        // Search keyword (may be natural language)
    MaxResults int    `json:"max_results"`  // Default 5, max 10
    Language   string `json:"language"`     // Default "zh"
    SearchType string `json:"search_type"`  // general | academic | news
}

type SearchResponse struct {
    RequestID string         `json:"request_id"`
    Status    string         `json:"status"`    // pending | completed | failed
    Results   []SearchResult `json:"results,omitempty"`
    Summary   string         `json:"summary"`   // LLM-generated summary of search results
    Duration  int64          `json:"duration"`  // Search duration (ms)
}

type SearchResult struct {
    Title   string `json:"title"`
    URL     string `json:"url"`
    Snippet string `json:"snippet"` // Summary snippet
    Source  string `json:"source"`  // Source website
}
```

### 6.3 HTTP API

#### 6.3.1 Initiate Search

`**POST /api/v1/search/query**`

**Request Body:** `SearchRequest`

**Response:**

```json
{
  "code": 200,
  "data": {
    "request_id": "search_12345678",
    "status": "completed",
    "results": [
      {
        "title": "The Essence of Linear Algebra - 3Blue1Brown",
        "url": "https://...",
        "snippet": "Linear transformation is the core concept of linear algebra...",
        "source": "bilibili.com"
      }
    ],
    "summary": "The search results indicate that the core teaching approach for linear algebra should revolve around the geometric intuition of linear transformations...",
    "duration": 2340
  }
}
```

If the search takes a long time (> 3s), return `status: "pending"` first, and let the caller poll for the result.

#### 6.3.2 Query Search Results

`**GET /api/v1/search/results/{request_id}**`

**Response:** same as the `SearchResponse` structure

### 6.4 Search Engine Backend Options


| Option | Description |
| --------------- | --------------------- |
| SerpAPI | Google Search API, paid |
| Bing Search API | Microsoft Search API, has a free quota |
| Tavily | Search API designed for AI Agents |
| DuckDuckGo | Free, can be used through scraping |


The concrete choice is up to the implementer, as long as it satisfies the API signatures in this chapter.

### 6.5 Enhanced Interaction Flow with KB/Memory

```
User speech → Voice Agent extracts search intent
    │
    ├─ goroutine A: KB.Query(query)     → contextQueue
    ├─ goroutine B: Memory.Recall(query) → contextQueue
    └─ goroutine C: WebSearch.Query(query) → (internal enhancement) → contextQueue
                          │
                          ├─ call search engine to get results
                          ├─ [optional] call KB.Query for deduplication
                          ├─ LLM refines the summary
                          └─ return enhanced results
```

Web Search Service may internally call KB's `/api/v1/kb/query` interface for deduplication checks (optional, implementation-specific).

### 6.6 Persist Search Results into the Knowledge Base

Search results should not be "used once and discarded". Valuable results should be asynchronously written back into the user's knowledge base so that the next identical/similar query can hit via RAG directly without searching again.

**Trigger Conditions (determined by Voice Agent):**

1. All chunk scores returned by the KB query are below the threshold (e.g. < 0.5), indicating the knowledge base does not cover the query
2. Web Search returns high-quality results (`status: completed` and `results` is not empty)

**Flow:**

```
Voice Agent goroutine:
    ├─ KB.Query(query) → low scores / no results
    └─ WebSearch.Query(query) → has results
           │
           ├─ 1. Inject results into contextQueue (answer the user in real time)
           └─ 2. Asynchronously call KB.IngestFromSearch (persist into KB)
                    └─ KB performs URL deduplication, chunking, and vectorization
```

**Voice Agent persistence call example:**

```go
go func() {
    if len(kbResult.Chunks) > 0 && kbResult.Chunks[0].Score >= 0.5 {
        return // KB already has sufficiently good content; no need to persist
    }
    if len(searchResp.Results) == 0 {
        return
    }
    items := make([]SearchIngestItem, 0, len(searchResp.Results))
    for _, r := range searchResp.Results {
        items = append(items, SearchIngestItem{
            Title:   r.Title,
            URL:     r.URL,
            Content: r.Snippet,
            Source:  r.Source,
        })
    }
    kbClient.IngestFromSearch(IngestFromSearchRequest{
        UserID: session.UserID,
        Items:  items,
    })
}()
```

This forms a closed loop that **gets smarter with use**: **search → answer → store → next time hit directly via RAG**.

### 6.7 Voice Agent Calling Pattern

```go
// Search is initiated only when the LLM determines it is needed
// (not every dialogue turn requires search)
// The Voice Agent's Large LLM triggers search through tool calling
p.asyncQuery(ctx, "web_search", func() (string, error) {
    resp, err := searchClient.Query(SearchRequest{
        RequestID:  NewID("search_"),
        UserID:     session.UserID,
        Query:      searchQuery,
        MaxResults: 5,
    })
    if err != nil { return "", err }
    return resp.Summary, nil
})
```

---

## Chapter 7 Database Service

This module is the persistent data layer of the entire system. It provides CRUD interfaces for user management, session management, task management, and file management, and integrates object storage for large files. Default port: `9500`.

### 7.1 Technology Stack Conventions


| Component | Selection |
| -------- | --------------------------------- |
| Relational Database | PostgreSQL 15+ |
| Object Storage | MinIO (local development) / Alibaba Cloud OSS / AWS S3 (production) |
| ORM / Driver | `gorm` |
| HTTP Framework | Gin / Chi |


### 7.2 Database Table Schemas (DDL)

#### 7.2.1 User Table

```sql
CREATE TABLE users (
    id          VARCHAR(64) PRIMARY KEY,         -- user_<uuid>
    username    VARCHAR(64) NOT NULL UNIQUE,
    email       VARCHAR(128) NOT NULL UNIQUE,
    password_hash VARCHAR(256) NOT NULL,
    display_name VARCHAR(128) DEFAULT '',
    subject     VARCHAR(64) DEFAULT '',          -- Teaching subject
    school      VARCHAR(128) DEFAULT '',
    role        VARCHAR(16) DEFAULT 'teacher',   -- teacher | admin
    created_at  BIGINT NOT NULL,                 -- Unix ms
    updated_at  BIGINT NOT NULL
);
```

#### 7.2.1A Pending Registration Table
```sql
CREATE TABLE pending_registrations (
    user_id                  VARCHAR(64) PRIMARY KEY,   -- 预分配最终 user_<uuid>
    username                 VARCHAR(64) NOT NULL UNIQUE,
    email                    VARCHAR(128) NOT NULL UNIQUE,
    password_hash            VARCHAR(256) NOT NULL,
    display_name             VARCHAR(128) DEFAULT '',
    subject                  VARCHAR(64) DEFAULT '',
    school                   VARCHAR(128) DEFAULT '',
    role                     VARCHAR(16) DEFAULT 'teacher',

    verification_token_hash  VARCHAR(128) NOT NULL UNIQUE, -- 仅存哈希，不存明文 token
    verification_expires_at  BIGINT NOT NULL,              -- Unix ms
    verification_sent_at     BIGINT NOT NULL,              -- Unix ms

    created_at               BIGINT NOT NULL,
    updated_at               BIGINT NOT NULL
);
CREATE INDEX idx_pending_regs_expires_at ON pending_registrations(verification_expires_at);
```

#### 7.2.2 Session Table

```sql
CREATE TABLE sessions (
    id          VARCHAR(64) PRIMARY KEY,         -- sess_<uuid>
    user_id     VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title       VARCHAR(256) DEFAULT '',
    status      VARCHAR(16) DEFAULT 'active',    -- active | completed | archived
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL
);
CREATE INDEX idx_sessions_user ON sessions(user_id);
```

#### 7.2.3 Task Table

```sql
CREATE TABLE tasks (
    id          VARCHAR(64) PRIMARY KEY,         -- task_<uuid>
    session_id  VARCHAR(64) NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    user_id     VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    topic       VARCHAR(256) NOT NULL,
    description TEXT DEFAULT '',
    total_pages INT DEFAULT 0,
    audience    VARCHAR(128) DEFAULT '',
    global_style VARCHAR(128) DEFAULT '',
    status      VARCHAR(16) DEFAULT 'pending',   -- pending | generating | completed | failed | exporting
    created_at  BIGINT NOT NULL,
    updated_at  BIGINT NOT NULL
);
CREATE INDEX idx_tasks_session ON tasks(session_id);
CREATE INDEX idx_tasks_user ON tasks(user_id);
```

#### 7.2.4 File Table

```sql
CREATE TABLE files (
    id          VARCHAR(64) PRIMARY KEY,         -- file_<uuid>
    user_id     VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    session_id  VARCHAR(64) REFERENCES sessions(id) ON DELETE SET NULL,
    task_id     VARCHAR(64) REFERENCES tasks(id) ON DELETE SET NULL,
    filename    VARCHAR(256) NOT NULL,
    file_type   VARCHAR(16) NOT NULL,            -- pdf | docx | pptx | image | video | html
    file_size   BIGINT NOT NULL,                 -- Number of bytes
    storage_url VARCHAR(1024) NOT NULL,          -- Object storage URL
    purpose     VARCHAR(32) DEFAULT 'reference', -- reference | export | knowledge_base | render
    created_at  BIGINT NOT NULL
);
CREATE INDEX idx_files_user ON files(user_id);
CREATE INDEX idx_files_task ON files(task_id);
```

#### 7.2.5 KB Document Metadata Table

```sql
CREATE TABLE kb_documents (
    id            VARCHAR(64) PRIMARY KEY,       -- doc_<uuid>
    collection_id VARCHAR(64) NOT NULL,          -- coll_<uuid>
    file_id       VARCHAR(64) REFERENCES files(id) ON DELETE SET NULL,
    user_id       VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title         VARCHAR(256) NOT NULL,
    doc_type      VARCHAR(16) NOT NULL,
    chunk_count   INT DEFAULT 0,
    status        VARCHAR(16) DEFAULT 'pending', -- pending | processing | indexed | failed
    error_message TEXT DEFAULT '',
    created_at    BIGINT NOT NULL
);
CREATE INDEX idx_kb_docs_collection ON kb_documents(collection_id);
CREATE INDEX idx_kb_docs_user ON kb_documents(user_id);
```

#### 7.2.6 Memory Entry Table

```sql
CREATE TABLE memory_entries (
    id                VARCHAR(64) PRIMARY KEY,   -- mem_<uuid>
    user_id           VARCHAR(64) NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category          VARCHAR(16) NOT NULL,      -- fact | preference | summary
    key               VARCHAR(128) NOT NULL,
    value             TEXT NOT NULL,
    context           VARCHAR(128) DEFAULT 'general',
    confidence        REAL DEFAULT 1.0,
    source            VARCHAR(16) DEFAULT 'explicit', -- explicit | inferred
    source_session_id VARCHAR(64) REFERENCES sessions(id) ON DELETE SET NULL,
    created_at        BIGINT NOT NULL,
    updated_at        BIGINT NOT NULL
);
CREATE INDEX idx_memory_user ON memory_entries(user_id);
CREATE INDEX idx_memory_user_category ON memory_entries(user_id, category);
CREATE UNIQUE INDEX idx_memory_user_key_context ON memory_entries(user_id, key, context);
```

### 7.3 HTTP API

#### 7.3.1 User Registration

`**POST /api/v1/auth/register**`

**Request Body:**

```go
type RegisterRequest struct {
    Username    string `json:"username"`     // Required
    Email       string `json:"email"`        // Required
    Password    string `json:"password"`     // Required, at least 8 characters
    DisplayName string `json:"display_name"`
    Subject     string `json:"subject"`
    School      string `json:"school"`
}
```

**Response:**

```json
{
  "code": 200,
  "message": "verification email sent",
  "data": {
    "user_id": "user_aabb1122",
    "verification_required": true,
    "verification_expires_at": 1710766400000
  }
}
```

**Error Codes:**


| code | Description |
| ----- | ------ |
| 40001 | Missing required field |
| 40901 | Username already exists |
| 40902 | Email already registered |


#### 7.3.2A Email Verification
`**POST /api/v1/auth/verify**`

**Request Body:**
```go
type VerifyEmailRequest struct {
    Token string `json:"token"` // Required, one-time opaque verification token
}
```

**Response:**
```json
{
  "code": 200,
  "message": "verified",
  "data": {
    "user_id": "user_aabb1122",
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "expires_at": 1710766400000
  }
}
```
| code  | Description                                            |
| ----- | ------------------------------------------------------ |
| 40001 | Missing token                                          |
| 40103 | Invalid verification token                             |
| 40104 | Verification token expired                             |
| 40903 | Registration already verified / token already consumed |
| 50000 | Internal error                                         |

#### 7.3.2 User Login

**`POST /api/v1/auth/login`**

**Request Body:**

```go
type LoginRequest struct {
    Username string `json:"username"` // choose one of username or email
    Email    string `json:"email"`
    Password string `json:"password"` // Required
}
```

**Response:**

```json
{
  "code": 200,
  "data": {
    "user_id": "user_aabb1122",
    "token": "eyJhbGciOiJIUzI1NiIs...",
    "expires_at": 1710766400000
  }
}
```
| code  | Description         |
| ----- | ------------------- |
| 40100 | Invalid credentials |
| 40102 | Email not verified  |



#### 7.3.3 Get User Information

`**GET /api/v1/auth/profile**`

- Authentication: `Authorization: Bearer <token>`, parse `user_id` from the token

**Response:**

```json
{
  "code": 200,
  "data": {
    "user_id": "user_aabb1122",
    "username": "zhangsan",
    "email": "zhang@example.com",
    "display_name": "Teacher Zhang",
    "subject": "Mathematics",
    "school": "Tsinghua University",
    "role": "teacher",
    "created_at": 1710000000000
  }
}
```

#### 7.3.4 File Upload (Object Storage Integration)

`**POST /api/v1/files/upload**`

- Content-Type: `multipart/form-data`
- Authentication: `Authorization: Bearer <token>`


| Form Field | Type | Required | Description |
| ------------ | ------ | --- | -------------------------------------------- |
| `file` | File | Yes | File |
| `session_id` | string | No | Associated session |
| `task_id` | string | No | Associated task |
| `purpose` | string | Yes | reference / export / knowledge_base / render |


**Processing Flow:**

1. Receive the file
2. Generate `file_<uuid>` as the file ID
3. Upload it to object storage with path format: `{user_id}/{purpose}/{file_id}_{filename}`
4. Insert a metadata record into the `files` table
5. Return file information

**Response:**

```json
{
  "code": 200,
  "data": {
    "file_id": "file_aabb1122",
    "filename": "lesson_plan.pdf",
    "file_type": "pdf",
    "file_size": 2048576,
    "storage_url": "https://oss.example.com/user_abc/reference/file_aabb1122_lesson_plan.pdf"
  }
}
```

#### 7.3.5 Get File Information

`**GET /api/v1/files/{file_id}**`

**Response:** return file metadata (same as the `data` part above) + `download_url` (a signed temporary URL)

#### 7.3.6 Delete File

**`DELETE /api/v1/files/{file_id}`**

Processing flow: delete the file in object storage → delete the record in the `files` table.

#### 7.3.7 Create Session

**`POST /api/v1/sessions`**

**Request Body:**

```go
type CreateSessionRequest struct {
    UserID string `json:"user_id"` // Parsed from token
    Title  string `json:"title"`
}
```

**Response:**

```json
{
  "code": 200,
  "data": {
    "session_id": "sess_aabb1122"
  }
}
```

#### 7.3.8 Get Session

`**GET /api/v1/sessions/{session_id}**`

#### 7.3.9 List User Sessions

`**GET /api/v1/sessions?user_id={user_id}&page=1&page_size=20**`

#### 7.3.10 Update Session

`**PUT /api/v1/sessions/{session_id}**`

**Request Body:**

```go
type UpdateSessionRequest struct {
    Title  string `json:"title,omitempty"`
    Status string `json:"status,omitempty"` // active | completed | archived
}
```

#### 7.3.11 Create Task

`**POST /api/v1/tasks**`

**Request Body:**

```go
type CreateTaskRequest struct {
    SessionID   string `json:"session_id"`
    UserID      string `json:"user_id"`
    Topic       string `json:"topic"`
    Description string `json:"description"`
    TotalPages  int    `json:"total_pages"`
    Audience    string `json:"audience"`
    GlobalStyle string `json:"global_style"`
}
```

**Response:**

```json
{
  "code": 200,
  "data": {
    "task_id": "task_abc123"
  }
}
```

#### 7.3.12 Get Task

`**GET /api/v1/tasks/{task_id}**`

#### 7.3.13 Update Task Status

`**PUT /api/v1/tasks/{task_id}/status**`

**Request Body:**

```go
type UpdateTaskStatusRequest struct {
    Status string `json:"status"` // pending | generating | completed | failed | exporting
}
```

#### 7.3.14 List Tasks Under a Session

`**GET /api/v1/tasks?session_id={session_id}&page=1&page_size=20**`

### 7.4 Cascade Deletion Rules


| Operation | Cascade Impact |
| ---- | ------------------------------------------------------------------------------------------------------------------ |
| Delete user | → delete all sessions → delete all tasks → delete `files` records (`files.user_id ON DELETE CASCADE`) → delete `memory_entries` → delete `kb_documents` |
| Delete session | → delete all tasks → `SET NULL` for `session_id` in `files` |
| Delete task | → `SET NULL` for `task_id` in `files` |
| Delete file | → delete the file from object storage → `SET NULL` for `file_id` in `kb_documents` |

Trigger requirements (to prevent garbage files in object storage):

1. When a `files` record is deleted (whether by explicit `DELETE /api/v1/files/{file_id}` or triggered by cascade deletion from `users`), `file_delete_jobs` must be written through a database trigger or Outbox mechanism.
2. The background cleanup Worker consumes `file_delete_jobs` and calls the object storage SDK to delete the object corresponding to `storage_url`.
3. Cleanup failures must be retried and alerted on, to avoid deleting only the database record while leaving stale data in object storage.


---

## Chapter 8 Reference Material Processing Pipeline

This chapter defines the unified multimodal document parsing interfaces, implemented by the Knowledge Base Service.

### 8.1 Unified Parsing Interface

```go
type DocumentProcessor interface {
    Parse(ctx context.Context, input ParseInput) (*ParsedDocument, error)
    SupportedTypes() []string
}

type ParseInput struct {
    FileURL  string `json:"file_url"`  // Object storage URL
    FileType string `json:"file_type"` // pdf | docx | pptx | image | video | text
    DocID    string `json:"doc_id"`
}

type ParsedDocument struct {
    DocID      string           `json:"doc_id"`
    FileType   string           `json:"file_type"`
    Title      string           `json:"title"`
    TextChunks []TextChunk      `json:"text_chunks"`
    Images     []ExtractedImage `json:"images,omitempty"`
    KeyFrames  []KeyFrame       `json:"key_frames,omitempty"`
    Tables     []ExtractedTable `json:"tables,omitempty"`
    Summary    string           `json:"summary"`
    TotalPages int              `json:"total_pages,omitempty"`
}
```

### 8.2 Processing Approach by Format

#### 8.2.1 PDF


| Step | Tool | Description |
| ------ | -------------------------------------- | ---------------------- |
| Text extraction | `pdfplumber` / `PyMuPDF` (through CGO or subprocess) | Extract text, tables, and images |
| Image extraction | `PyMuPDF` | Extract embedded images |
| OCR fallback | `PaddleOCR` / Tesseract | Text recognition for scanned PDFs |
| Chunking | By paragraph/section | 500-1000 characters per chunk, 100-character overlap |


#### 8.2.2 Word (.docx)


| Step | Tool | Description |
| ---- | -------------------------------- | ---------- |
| Text extraction | `go-docx` or Python `python-docx` | Extract paragraphs, headings, and tables |
| Image extraction | Parse the `media/` directory inside docx | Extract embedded images |
| Chunking | Chunk by heading hierarchy | Preserve chapter structure |


#### 8.2.3 PPT (.pptx)


| Step | Tool | Description |
| ---- | ------------- | --------- |
| Text extraction | `python-pptx` | Extract textbox content page by page |
| Image extraction | `python-pptx` | Extract images on each page |
| Chunking | One chunk per page | Preserve page number information |


#### 8.2.4 Image


| Step | Tool | Description |
| ----- | ------------ | ----------------------- |
| OCR | `PaddleOCR` | Text recognition |
| Multimodal description | LLM (vision) | Describe image content |
| Chunking | One chunk for the whole image | Use OCR text + image description as `content` |


#### 8.2.5 Video


| Step | Tool | Description |
| ----- | ---------------------- | ------------------------- |
| Key frame extraction | `ffmpeg` | Extract key frames by scene change or fixed interval |
| Audio extraction | `ffmpeg` | Separate the audio track |
| Speech-to-text | ASR (Whisper / FunASR) | Audio → transcript |
| Key frame description | LLM (vision) | Describe key frame content |
| Chunking | By time segment | One chunk every 30-60 seconds, including transcript + key frame description |


### 8.3 Auxiliary Data Structures

```go
type ExtractedImage struct {
    ImageID     string `json:"image_id"`
    ImageURL    string `json:"image_url"`    // URL after upload to object storage
    PageNumber  int    `json:"page_number"`
    Description string `json:"description"`  // LLM-generated image description
    OCRText     string `json:"ocr_text"`     // Text extracted by OCR
    Width       int    `json:"width"`
    Height      int    `json:"height"`
}

type KeyFrame struct {
    FrameID     string  `json:"frame_id"`
    ImageURL    string  `json:"image_url"`
    Timestamp   float64 `json:"timestamp"`    // Time point in the video (seconds)
    Description string  `json:"description"`  // LLM-generated frame description
    Transcript  string  `json:"transcript"`   // Speech text for the corresponding time segment
}

type ExtractedTable struct {
    TableID    string     `json:"table_id"`
    PageNumber int        `json:"page_number"`
    Headers    []string   `json:"headers"`
    Rows       [][]string `json:"rows"`
    Markdown   string     `json:"markdown"` // Markdown text representation of the table
}
```

### 8.4 Chunking Strategy

All documents follow the rules below during chunking:


| Parameter | Default | Description |
| ---------------- | ----------- | ----------------------------------------- |
| `chunk_size` | 800 characters | Maximum number of characters per chunk |
| `chunk_overlap` | 100 characters | Number of overlapping characters between chunks |
| `split_by` | `paragraph` | Splitting basis: paragraph / heading / page |
| `min_chunk_size` | 100 characters | Minimum characters per chunk; if smaller, merge into the previous chunk |


```go
type ChunkConfig struct {
    ChunkSize    int    `json:"chunk_size"`
    ChunkOverlap int    `json:"chunk_overlap"`
    SplitBy      string `json:"split_by"`
    MinChunkSize int    `json:"min_chunk_size"`
}

func DefaultChunkConfig() ChunkConfig {
    return ChunkConfig{
        ChunkSize:    800,
        ChunkOverlap: 100,
        SplitBy:      "paragraph",
        MinChunkSize: 100,
    }
}
```

### 8.5 Processing Pipeline HTTP API

Document processing can be triggered internally through Knowledge Base Service or called independently:

`**POST /api/v1/kb/parse**` - Parse a document only (without inserting into the vector store)

**Request Body:** `ParseInput`

**Response:** `ParsedDocument`

This interface is also used by PPT Agent (to parse teacher-uploaded reference PPTs and extract layout style).

---

## Appendix A Team Responsibilities


| Module | Owner | Interface Chapters to Implement | Dependencies | Language / Framework |
| ----------------------- | --- | --------- | ------------------------------------- | --------------- |
| Voice Agent + Message Bus + Frontend |  | Chapters 0/1/2 | HTTP clients for all modules | Go |
| PPT Agent + Task Management (Tasks) |  | Chapter 3 + Chapter 7 Tasks | Redis, Canvas Renderer, LLM | Go + Python (rendering) |
| Knowledge Base |  | Chapters 4/8 | Database Service, vector database, embedding model | Go + Python (parsing) |
| Storage / Session / Search (Files + Sessions + Web Search) |  | Chapters 6/7 (except Tasks/Auth) | PostgreSQL, MinIO/S3, Search API | Go |
| User and Memory Service (Auth + Memory) |  | Chapter 5 + Chapter 7 Auth | PostgreSQL, Redis, LLM | Go |


### Development Priorities

1. **P0 (Week 1)**: Database Service (Chapter 7) - the foundation all modules depend on
2. **P0 (Week 1)**: Voice Agent message bus (Chapter 1) - core orchestration capability
3. **P1 (Week 2)**: Knowledge Base (Chapters 4/8) + Memory Service (Chapter 5) - parallel development
4. **P1 (Week 2)**: PPT Agent (Chapter 3) - mock data can be used first
5. **P2 (Week 3)**: Web Search (Chapter 6) + frontend refinement (Chapter 2) + integration testing

---

## Appendix B System Architecture Diagram

```mermaid
graph TB
    subgraph frontend [Frontend Browser]
        UI[Web UI<br/>Voice/Text Input + PPT Preview]
    end

    subgraph voiceAgent [Voice Agent - Port 9000]
        WS[WebSocket Handler]
        Session[Session State Machine]
        Pipe[Pipeline<br/>ASR→LLM→TTS]
        CQ[Context Injection Queue]
        HPQ[High Priority Queue]
    end

    subgraph pptAgent [PPT Agent - Port 9100]
        PPTInit[Task Initialization]
        Feedback[Feedback Processing]
        Merger[Three-Way Merge Engine]
        Renderer[Canvas Renderer]
        Export[Export Engine]
    end

    subgraph kbService [Knowledge Base - Port 9200]
        DocParser[Document Parsing Pipeline]
        Embedder[Embedding Engine]
        VectorDB[Vector Database<br/>Milvus/Qdrant]
        Retriever[Semantic Retrieval]
    end

    subgraph memService [Memory Service - Port 9300]
        MemExtract[Memory Extraction LLM]
        MemRecall[Memory Recall]
        MemProfile[User Profile]
    end

    subgraph searchService [Web Search - Port 9400]
        SearchEngine[Search Engine API]
        Refiner[LLM Result Refinement]
    end

    subgraph dbService [Database Service - Port 9500]
        UserMgmt[User Management]
        FileMgmt[File Management]
        SessionMgmt[Session Management]
        TaskMgmt[Task Management]
    end

    subgraph storage [Infrastructure]
        PG[(PostgreSQL)]
        RedisDB[(Redis)]
        OSS[(MinIO/S3<br/>Object Storage)]
    end

    UI -->|WebSocket| WS
    WS --> Session --> Pipe
    Pipe --> CQ
    HPQ --> Pipe

    Pipe -.->|"async"| Retriever
    Pipe -.->|"async"| MemRecall
    Pipe -.->|"async"| SearchEngine

    Retriever -.->|ContextMessage| CQ
    MemRecall -.->|ContextMessage| CQ
    Refiner -.->|ContextMessage| CQ

    Pipe -->|HTTP POST| PPTInit
    Pipe -->|HTTP POST| Feedback
    Merger -->|HTTP POST reverse assistance| HPQ

    DocParser --> Embedder --> VectorDB
    MemExtract --> PG
    MemRecall --> PG
    MemRecall --> RedisDB

    dbService --> PG
    dbService --> OSS
    Merger --> RedisDB
    Renderer --> OSS
end
```



---

**End of Document. All module implementers must develop strictly according to the interface signatures and data structures defined in this document. Any interface change must be approved by the team lead before this document is updated in a unified manner.**
