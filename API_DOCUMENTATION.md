# Voice Agent 接口文档

本文档详细描述 Voice Agent 系统的所有接口，包括：

- **我们需要外部实现的接口**（Voice Agent 作为客户端调用）
- **我们提供的接口**（外部系统调用 Voice Agent）

---

## 目录

### 第一部分：我们需要外部实现的接口

1. [PPT Agent 服务接口](#1-ppt-agent-服务接口)
2. [知识库服务接口](#2-知识库服务接口)
3. [记忆服务接口](#3-记忆服务接口)
4. [搜索服务接口](#4-搜索服务接口)
5. [数据库服务接口](#5-数据库服务接口)
6. [认证服务接口](#6-认证服务接口)
7. [会话管理接口](#7-会话管理接口)

### 第二部分：我们提供的接口

1. [WebSocket 连接接口](#websocket-连接接口)
2. [HTTP REST 接口](#http-rest-接口)

---

> **上下文长度判断说明**: voice agent 的 `ConversationHistory` 以字符数估算 token 用量（中文约 1.5 char/token，英文约 4 char/token）。当历史消息累计字符数超过 **8000 字符**时触发压缩，将最老的 1/3 消息 push 给记忆模块后从本地删除。

---

# 第一部分：我们需要外部实现的接口

Voice Agent 作为客户端，需要调用以下外部服务的接口。

---

## 1. PPT Agent 服务接口

### 1.1 初始化 PPT 任务

**接口路径**: `POST /api/v1/ppt/init`

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "session_id": "string",        // 必填，会话ID
  "topic": "string",             // 必填，课程主题
  "description": "string",       // 必填，详细描述（包含教学目标、知识点等结构化信息）
  "total_pages": 0,              // 必填，期望页数，整数，>0
  "audience": "string",          // 必填，目标受众
  "global_style": "string",      // 必填，全局风格
  "teaching_elements": {         // 必填，教学元素
    "knowledge_points": ["string"],      // 必填，知识点列表
    "teaching_goals": ["string"],        // 必填，教学目标列表
    "teaching_logic": "string",          // 必填，讲授逻辑
    "key_difficulties": ["string"],      // 必填，重点难点列表
    "duration": "string",                // 必填，课时长度，如 "45分钟"
    "interaction_design": "string",      // 必填，互动设计
    "output_formats": ["string"]         // 必填，输出格式列表，如 ["pptx", "pdf"]
  },
  "reference_files": [           // 可选，参考文件列表
    {
      "file_id": "string",       // 必填，文件ID
      "file_url": "string",      // 必填，文件URL
      "file_type": "string",     // 必填，文件类型，如 "pdf", "docx", "image"
      "instruction": "string"    // 必填，使用说明
    }
  ]
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": "string",         // 必填，任务ID
    "status": "string"           // 必填，任务状态，可选值: "created", "processing", "completed", "failed"
  }
}
```

**使用示例**:

```bash
curl -X POST http://ppt-agent-url/api/v1/ppt/init \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_abc123",
    "topic": "高等数学-导数与微分",
    "description": "理解导数概念，掌握求导法则",
    "total_pages": 20,
    "audience": "大学一年级学生",
    "global_style": "简洁专业",
    "teaching_elements": {
      "knowledge_points": ["导数定义", "求导公式", "链式法则"],
      "teaching_goals": ["理解导数概念", "掌握求导法则"],
      "teaching_logic": "从概念到应用，循序渐进",
      "key_difficulties": ["链式法则的应用"],
      "duration": "45分钟",
      "interaction_design": "每5页插入一个练习题",
      "output_formats": ["pptx"]
    }
  }'
```

---

### 1.2 发送 PPT 修改反馈

**接口路径**: `POST /api/v1/ppt/feedback`

**请求参数**:

```json
{
  "task_id": "string",           // 必填，任务ID
  "base_timestamp": 0,           // 必填，基准时间戳（毫秒），用于版本控制
  "viewing_page_id": "string",   // 必填，当前查看的页面ID
  "raw_text": "string",          // 必填，用户原始语音/文本输入
  "intents": [                   // 可选，意图列表（可由 PPT Agent 自行解析）
    {
      "action_type": "string",   // 必填，操作类型，可选值: "modify", "insert", "delete", "reorder", "style"
      "target_page_id": "string",// 必填，目标页面ID
      "instruction": "string"    // 必填，具体指令
    }
  ]
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success"
}
```

**使用示例**:

```bash
curl -X POST http://ppt-agent-url/api/v1/ppt/feedback \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "base_timestamp": 1711545600000,
    "viewing_page_id": "page_003",
    "raw_text": "把第三页的标题改成蓝色",
    "intents": []
  }'
```

---

### 1.3 获取画布状态

**接口路径**: `GET /api/v1/canvas/status?task_id={task_id}`

**请求参数**:

- `task_id` (query string): 必填，任务ID

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": "string",
    "page_order": ["string"],
    "current_viewing_page_id": "string",
    "pages_info": [
      {
        "page_id": "string",
        "status": "string",               // "rendering", "completed", "failed", "suspended_for_human"
        "last_update": 0,
        "render_url": "string"
      }
    ]
  }
}
```

**使用示例**:

```bash
curl -X GET "http://ppt-agent-url/api/v1/canvas/status?task_id=task_001"
```

---

### 1.4 通知 VAD 事件

**接口路径**: `POST /api/v1/canvas/vad-event`

**请求参数**:

```json
{
  "task_id": "string",
  "timestamp": 0,
  "viewing_page_id": "string"
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success"
}
```

**说明**: Voice Agent 在用户开始说话（VAD Start）时发送此事件，用于同步用户的交互时间点。

**使用示例**:

```bash
curl -X POST http://ppt-agent-url/api/v1/canvas/vad-event \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "timestamp": 1711545600000,
    "viewing_page_id": "page_005"
  }'
```

---

## 2. 知识库服务接口

> **架构说明**: kb-service 同时服务两类调用方：
> - **voice agent 调用** `POST /api/v1/kb/query` → 异步受理，结果通过 `ppt_message` 回调（`msg_type: "kb_result"`）返回 summary
> - **记忆模块 / PPT Agent 调用** `POST /api/v1/kb/query-chunks` → 同步返回 `[]chunk`，用于 RAG 检索
>   - 传 `user_id`：同时检索用户个人知识库
>   - 不传 `user_id`：仅检索专业知识库

### 2.1 专业知识查询（voice agent 调用）

**接口路径**: `POST /api/v1/kb/query`

**说明**: 异步受理，立即返回 `accepted: true`，检索完成后回调 `POST /api/v1/voice/ppt_message`（`msg_type: "kb_result"`）。

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "session_id": "string",        // 必填，会话ID（用于关联回调）
  "query": "string",             // 必填，查询内容
  "top_k": 5,                    // 可选，默认5
  "score_threshold": 0.7         // 可选，0.0-1.0
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": { "accepted": true }
}
```

**回调格式**:

```json
{
  "task_id": "string",           // 对应 session_id
  "msg_type": "kb_result",
  "summary": "string"
}
```

**使用示例**:

```bash
curl -X POST http://kb-service-url/api/v1/kb/query \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_abc123",
    "query": "导数的几何意义是什么",
    "top_k": 5
  }'
```

**接口路径**: `POST /api/v1/kb/query`

**调用方**: voice agent，用于获取专业知识库的摘要回答。

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "query": "string",             // 必填，查询内容
  "top_k": 5,                    // 可选，参与摘要的最大 chunk 数，默认5
  "score_threshold": 0.7         // 可选，相似度阈值，0.0-1.0
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "summary": "string"          // 必填，基于检索结果生成的摘要文本
  }
}
```

**使用示例**:

```bash
curl -X POST http://kb-service-url/api/v1/kb/query \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "query": "导数的几何意义是什么",
    "top_k": 5,
    "score_threshold": 0.7
  }'
```

---

### 2.2 关键词 chunk 检索（供记忆模块 / PPT Agent 调用）

**接口路径**: `POST /api/v1/kb/query-chunks`

**调用方**: 个人记忆模块（memory-service）或 PPT Agent，不由 voice agent 直接调用。

**请求参数**:

```json
{
  "user_id": "string",           // 可选，用户ID；传入则同时检索用户个人知识库，不传则仅检索专业知识库
  "keywords": ["string"],        // 必填，关键词切片列表
  "top_k": 5,                    // 可选，每个关键词返回的最大 chunk 数，默认5
  "score_threshold": 0.7         // 可选，相似度阈值，0.0-1.0
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "chunks": [
      {
        "chunk_id": "string",
        "content": "string",
        "source": "string",      // 来源标识（如 OSS 路径、文件名）
        "score": 0.0,
        "metadata": {}
      }
    ],
    "total": 0
  }
}
```

**使用示例**（记忆模块调用，带 user_id）:

```bash
curl -X POST http://kb-service-url/api/v1/kb/query-chunks \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "keywords": ["导数", "几何意义", "切线斜率"],
    "top_k": 5,
    "score_threshold": 0.7
  }'
```

**使用示例**（PPT Agent 调用，不带 user_id，仅查专业知识库）:

```bash
curl -X POST http://kb-service-url/api/v1/kb/query-chunks \
  -H "Content-Type: application/json" \
  -d '{
    "keywords": ["量子力学", "波粒二象性"],
    "top_k": 10
  }'
```

---

### 2.2 从搜索结果导入知识库

**接口路径**: `POST /api/v1/kb/ingest-from-search`

**调用方**: 搜索服务（search-service）。搜索完成后直接调用此接口将结果写入 kb-service，无需回调 voice agent。

**请求参数**:

```json
{
  "user_id": "string",           // 可选，有则写入用户个人知识库，无则写入公共专业知识库
  "collection_id": "string",     // 可选，集合ID
  "items": [
    {
      "title": "string",         // 必填，搜索结果标题
      "url": "string",           // 必填，搜索结果页面URL
      "content": "string",       // 必填，搜索结果摘要/正文
      "source": "string"         // 必填，固定为 "web_search"
    }
  ]
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success"
}
```

**使用示例**:

```bash
curl -X POST http://kb-service-url/api/v1/kb/ingest-from-search \
  -H "Content-Type: application/json" \
  -d '{
    "collection_id": "math_collection",
    "items": [
      {
        "title": "导数的定义与性质",
        "url": "https://example.com/calculus",
        "content": "导数是函数在某一点的瞬时变化率...",
        "source": "web_search"
      }
    ]
  }'
```

---

## 3. 记忆服务接口

> **架构说明**: 个人记忆模块是**控制面**。voice agent 通过工具调用触发记忆召回，记忆模块立即返回 `success`，然后异步将 query 拆成关键词切片 push 给 kb-service 做检索，检索结果通过回调返回给 voice agent。上下文历史压缩也由记忆模块负责：接收 voice agent 推送的老消息后，按 user_id 隔离并上传 OSS，作为 kb-service 后续嵌入检索的原始数据。

### 3.1 触发记忆召回（异步）

**接口路径**: `POST /api/v1/memory/recall`

**说明**: 本接口为**异步受理**。记忆模块收到请求后立即返回 `success`，随后在后台将 query 拆分为关键词切片并 push 给 kb-service 检索，检索完成后通过回调 `POST /api/v1/voice/ppt_message`（`msg_type: "kb_result"`）将结果返回给 voice agent。

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "session_id": "string",        // 必填，会话ID（用于关联回调）
  "query": "string",             // 必填，原始查询内容（记忆模块负责拆分为关键词）
  "top_k": 5                     // 可选，每个关键词返回的最大 chunk 数，默认5
}
```

**响应格式**（受理成功，异步处理中）:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "accepted": true
  }
}
```

**回调格式**: 检索完成后，记忆模块回调 voice agent 的 `POST /api/v1/voice/ppt_message`：

```json
{
  "task_id": "string",           // 对应 session_id
  "msg_type": "kb_result",
  "summary": "string"            // 必填，基于检索结果生成的摘要文本
}
```

**使用示例**:

```bash
curl -X POST http://memory-service-url/api/v1/memory/recall \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_abc123",
    "query": "用户的教学风格偏好",
    "top_k": 5
  }'
```

---

### ~~3.2 获取用户画像~~（已废弃）

> **废弃原因**: 用户画像信息通过 `POST /api/v1/memory/recall` 的回调 summary 携带，无需单独拉取。

---

### 3.3 推送上下文历史（触发压缩）

**接口路径**: `POST /api/v1/memory/context/push`

**说明**: voice agent 在以下两种时机调用此接口：
1. **超阈值压缩**：对话历史累计字符数 > 8000 时，将最老的 1/3 消息 push 后从本地 history 删除
2. **会话结束**：WebSocket 断开时，将剩余全部历史消息 push（不删除，会话已结束）

记忆模块收到后：按 user_id 隔离数据、上传 OSS、异步触发 kb-service 嵌入索引、更新用户画像的 `history_summary`。

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID（数据隔离依据）
  "session_id": "string",        // 必填，会话ID
  "messages": [                  // 必填，待归档的历史消息（按时间顺序，最老的在前）
    {
      "role": "string",          // "user" 或 "assistant"
      "content": "string"
    }
  ]
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "accepted": true,
    "message_count": 0           // 实际接收的消息条数
  }
}
```

**使用示例**:

```bash
curl -X POST http://memory-service-url/api/v1/memory/context/push \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_abc123",
    "messages": [
      {"role": "user", "content": "我想做一个关于导数的课件"},
      {"role": "assistant", "content": "好的，请问目标受众是哪个年级？"}
    ]
  }'
```

---

### ~~3.4 提取记忆~~（已废弃）

> **废弃原因**: 上下文历史压缩统一通过 `POST /api/v1/memory/context/push` 完成，记忆提取由记忆模块内部处理，voice agent 不再主动调用此接口。

---

### ~~3.5 保存工作记忆~~（已废弃）

> **废弃原因**: 工作记忆的维护由记忆模块在处理 `context/push` 和 `recall` 时内部完成，voice agent 不再主动调用。

---

### ~~3.6 获取工作记忆~~（已废弃）

> **废弃原因**: voice agent 通过 `recall` 回调获取所需上下文，不再单独拉取工作记忆。

---

## 4. 搜索服务接口

### 4.1 网络搜索

**接口路径**: `POST /api/v1/search/query`

**说明**: 异步任务受理。搜索完成后回调 `POST /api/v1/voice/ppt_message`，`msg_type` 为 `"search_result"`。

**请求参数**:

```json
{
  "request_id": "string",        // 可选，不提供则由服务生成
  "user_id": "string",           // 必填
  "session_id": "string",        // 可选；建议 Voice Agent 传入当前 WebSocket 会话 ID，便于异步完成后回调 `ppt_message` 时定位会话（task_id 使用 session_id）
  "query": "string",             // 必填
  "max_results": 10,             // 可选，默认10，上限10
  "language": "string"           // 可选，如 "zh-CN"
}
```

**部署说明（search-service）**: 配置环境变量 `VOICE_AGENT_BASE_URL`（无尾部斜杠，例如 `http://voice-agent:8080`）后，搜索任务结束会向 `POST /api/v1/voice/ppt_message` 发送 `msg_type: "search_result"` 或失败时的 `error`。配置 `KB_INGEST_URL`（例如 `http://kb-service:9200/api/v1/kb/ingest-from-search`）后，在检索到网页条目时会调用知识库「从搜索结果导入」接口回注入库。

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "request_id": "string",
    "status": "pending",
    "results": [],
    "summary": "",
    "duration": 0
  }
}
```

**使用示例**:

```bash
curl -X POST http://search-service-url/api/v1/search/query \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "query": "导数的应用",
    "max_results": 5,
    "language": "zh-CN"
  }'
```

---

## 5. 数据库服务接口

### 5.1 文件上传

**接口路径**: `POST /api/v1/files/upload`

**请求参数**: `multipart/form-data`，表单字段为文件数据。

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "file_id": "string",
    "filename": "string",
    "file_type": "string",
    "file_size": 0,
    "storage_url": "string",
    "purpose": "string"
  }
}
```

**使用示例**:

```bash
curl -X POST http://db-service-url/api/v1/files/upload \
  -H "Content-Type: multipart/form-data" \
  -F "file=@/path/to/document.pdf"
```

---

### 5.2 获取文件元信息

**接口路径**: `GET /api/v1/files/:file_id`

**请求参数**:

- `file_id` (path parameter): 必填，文件ID

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "file_id": "string",
    "filename": "string",
    "file_type": "string",
    "file_size": 0,
    "storage_url": "string",
    "purpose": "string"
  }
}
```

**使用示例**:

```bash
curl -X GET http://db-service-url/api/v1/files/file_001
```

---

### 5.3 删除文件

**接口路径**: `DELETE /api/v1/files/:file_id`

**请求参数**:

- `file_id` (path parameter): 必填，文件ID

**响应格式**:

```json
{
  "code": 200,
  "message": "success"
}
```

**使用示例**:

```bash
curl -X DELETE http://db-service-url/api/v1/files/file_001
```

---

## 6. 认证服务接口

### 6.1 验证用户 Token

**接口路径**: `POST /api/v1/auth/verify`

**请求参数**:

```json
{
  "token": "string"
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "user_id": "string",
    "valid": true
  }
}
```

**使用示例**:

```bash
curl -X POST http://auth-service-url/api/v1/auth/verify \
  -H "Content-Type: application/json" \
  -d '{"token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."}'
```

---

### 6.2 获取用户基础信息

**接口路径**: `GET /api/v1/auth/profile`

**请求参数**: Header `Authorization: Bearer {token}`

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "user_id": "string",
    "username": "string",
    "email": "string",
    "display_name": "string",
    "avatar_url": "string",
    "created_at": 0
  }
}
```

**使用示例**:

```bash
curl -X GET http://auth-service-url/api/v1/auth/profile \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
```

---

## 7. 会话管理接口

### 7.1 创建会话

**接口路径**: `POST /api/v1/sessions`

**请求参数**:

```json
{
  "user_id": "string",
  "session_id": "string",        // 可选，不提供则自动生成
  "title": "string"              // 可选
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "session_id": "string",
    "user_id": "string",
    "title": "string",
    "status": "string",          // "active", "archived", "completed"
    "created_at": 0
  }
}
```

**使用示例**:

```bash
curl -X POST http://session-service-url/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{"user_id": "user_001", "title": "高等数学课件制作"}'
```

---

### 7.2 获取会话详情

**接口路径**: `GET /api/v1/sessions/{session_id}`

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "session_id": "string",
    "user_id": "string",
    "title": "string",
    "status": "string",
    "created_at": 0,
    "updated_at": 0
  }
}
```

**使用示例**:

```bash
curl -X GET http://session-service-url/api/v1/sessions/sess_abc123
```

---

### 7.3 获取用户会话列表

**接口路径**: `GET /api/v1/sessions?user_id={user_id}&page=1&page_size=20`

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "sessions": [
      {
        "session_id": "string",
        "user_id": "string",
        "title": "string",
        "status": "string",
        "created_at": 0,
        "updated_at": 0
      }
    ],
    "total": 0,
    "page": 1
  }
}
```

**使用示例**:

```bash
curl -X GET "http://session-service-url/api/v1/sessions?user_id=user_001&page=1&page_size=20"
```

---

### 7.4 更新会话

**接口路径**: `PUT /api/v1/sessions/{session_id}`

**请求参数**:

```json
{
  "title": "string",             // 可选
  "status": "string"             // 可选，"active", "archived", "completed"
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success"
}
```

**使用示例**:

```bash
curl -X PUT http://session-service-url/api/v1/sessions/sess_abc123 \
  -H "Content-Type: application/json" \
  -d '{"title": "高等数学课件（已完成）", "status": "completed"}'
```

---

# 第二部分：我们提供的接口

Voice Agent 对外提供以下接口供其他系统调用。

---

## WebSocket 连接接口

### 建立 WebSocket 连接

**接口路径**: `ws://voice-agent-url/ws?user_id={user_id}&session_id={session_id}`

**请求参数**:

- `user_id` (query): 必填
- `session_id` (query): 可选，不提供则自动生成

支持二进制消息（音频）和文本消息（JSON）。

---

### WebSocket 消息格式

#### 客户端 → 服务端

**VAD 开始**: `{"type": "vad_start"}`

**VAD 结束**: `{"type": "vad_end"}`

**文本输入**:
```json
{"type": "text_input", "text": "string"}
```

**页面导航**:
```json
{"type": "page_navigate", "task_id": "string", "page_id": "string"}
```

**音频数据**: 二进制，PCM 16kHz 16bit 单声道。

---

#### 服务端 → 客户端

**状态更新**:
```json
{"type": "status", "state": "string"}
```
`state` 可选值: `"idle"`, `"listening"`, `"processing"`, `"speaking"`

**转录文本（实时）**:
```json
{"type": "transcript", "text": "string"}
```

**转录文本（最终）**:
```json
{"type": "transcript_final", "text": "string"}
```

**响应文本（流式）**:
```json
{"type": "response", "text": "string"}
```

**音频数据**: 二进制，MP3。

**任务状态更新**:
```json
{"type": "task_status", "task_id": "string", "status": "string", "progress": 0, "text": "string"}
```

**页面渲染完成**:
```json
{"type": "page_rendered", "task_id": "string", "page_id": "string", "render_url": "string", "page_index": 0}
```

**PPT 预览**:
```json
{
  "type": "ppt_preview",
  "task_id": "string",
  "page_order": ["string"],
  "pages_info": [
    {"page_id": "string", "status": "string", "last_update": 0, "render_url": "string"}
  ]
}
```

**导出就绪**:
```json
{"type": "export_ready", "task_id": "string", "download_url": "string", "format": "string"}
```

**冲突询问**:
```json
{"type": "conflict_ask", "task_id": "string", "page_id": "string", "context_id": "string", "question": "string"}
```

**需求收集进度**:
```json
{
  "type": "requirements_progress",
  "status": "string",            // "collecting" 或 "ready"
  "collected_fields": ["string"],
  "missing_fields": ["string"],
  "requirements": {}
}
```

**需求收集完成摘要**:
```json
{"type": "requirements_summary", "summary_text": "string", "requirements": {}}
```

**任务列表更新**:
```json
{"type": "task_list_update", "active_task_id": "string", "tasks": {"task_id": "topic"}}
```

**错误消息**:
```json
{"type": "error", "code": 0, "message": "string"}
```

---

## HTTP REST 接口

### 1. 文件上传（代理）

**接口路径**: `POST /api/v1/upload`

透明代理到 db-service 的 `/api/v1/files/upload`。

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "file_id": "string",
    "filename": "string",
    "file_type": "string",
    "file_size": 0,
    "storage_url": "string",
    "purpose": "string"
  }
}
```

**使用示例**:

```bash
curl -X POST http://voice-agent-url/api/v1/upload \
  -H "Content-Type: multipart/form-data" \
  -F "file=@/path/to/document.pdf"
```

---

### 2. 获取任务预览

**接口路径**: `GET /api/v1/tasks/{task_id}/preview`

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": "string",
    "status": "string",          // "generating", "completed", "failed"
    "page_order": ["string"],
    "current_viewing_page_id": "string",
    "pages": [
      {
        "page_id": "string",
        "status": "string",      // "rendering", "completed", "failed", "suspended_for_human"
        "last_update": 0,
        "render_url": "string"
      }
    ]
  }
}
```

**使用示例**:

```bash
curl -X GET http://voice-agent-url/api/v1/tasks/task_001/preview
```

---

### 3. 接收异步服务回调

**接口路径**: `POST /api/v1/voice/ppt_message`

**请求参数**:

```json
{
  "task_id": "string",           // 必填，任务ID（或 session_id）
  "msg_type": "string",          // 可选，默认 "tool_result"
  "priority": "string",          // 可选，"normal" 或 "high"，默认 "normal"
  "tts_text": "string",          // 可选，TTS文本
  "status": "string",            // 可选，用于 ppt_status 类型
  "progress": 0,                 // 可选，进度 0-100
  "page_id": "string",           // 可选
  "context_id": "string",        // 可选，用于冲突解决
  "render_url": "string",        // 可选，用于 page_rendered
  "page_index": 0,               // 可选
  "page_order": ["string"],      // 可选，用于 ppt_preview
  "pages_info": [],              // 可选，用于 ppt_preview
  "download_url": "string",      // 可选，用于 export_ready
  "format": "string",            // 可选，用于 export_ready
  "error_code": 0,               // 可选，用于 error
  "summary": "string"            // 可选，用于 kb_result（记忆模块回调的检索摘要）
}
```

**msg_type 可选值**:

| 值 | 说明 |
|---|---|
| `tool_result` | 工具执行结果（默认） |
| `ppt_status` | PPT 状态更新 |
| `page_rendered` | 页面渲染完成 |
| `ppt_preview` | PPT 预览 |
| `export_ready` | 导出就绪 |
| `conflict_question` | 冲突询问（自动设为高优先级，推送 `conflict_ask` 给客户端） |
| `conflict_resolved` | 冲突已解决（voice agent 将用户答案通过 `SendFeedback` 转发给 PPT Agent） |
| `search_result` | 搜索服务回调结果 |
| `kb_result` | 记忆模块触发 kb 检索后的回调结果 |
| `error` | 错误消息 |

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "accepted": true,
    "delivered": true            // 会话不存在时为 false
  }
}
```

**使用示例**（PPT 状态更新）:

```bash
curl -X POST http://voice-agent-url/api/v1/voice/ppt_message \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "ppt_status",
    "status": "generating",
    "progress": 50,
    "tts_text": "正在生成第10页"
  }'
```

**使用示例**（记忆模块 kb 检索回调）:

```bash
curl -X POST http://voice-agent-url/api/v1/voice/ppt_message \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "sess_abc123",
    "msg_type": "kb_result",
    "chunks": [
      {"chunk_id": "c1", "content": "导数是函数的瞬时变化率...", "source": "oss://user_001/history_001.txt", "score": 0.92}
    ],
    "profile_summary": "高中数学教师，偏好简洁风格"
  }'
```

---
