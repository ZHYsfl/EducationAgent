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

### 第二部分：我们提供的接口

1. [WebSocket 连接接口](#websocket-连接接口)
2. [HTTP REST 接口](#http-rest-接口)

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
  "teaching_elements": {         // 可选，教学元素
    "knowledge_points": ["string"],      // 可选，知识点列表
    "teaching_goals": ["string"],        // 可选，教学目标列表
    "teaching_logic": "string",          // 可选，讲授逻辑
    "key_difficulties": ["string"],      // 可选，重点难点列表
    "duration": "string",                // 可选，课时长度，如 "45分钟"
    "interaction_design": "string",      // 可选，互动设计
    "output_formats": ["string"]         // 可选，输出格式列表，如 ["pptx", "pdf"]
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
    "description": "【课程主题】高等数学-导数与微分\n【教学目标】理解导数概念；掌握求导法则\n【核心知识点】导数定义、求导公式、链式法则",
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
      "action_type": "string",   // 必填，操作类型，如 "modify_content", "change_style", "add_page"
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
    "task_id": "string",                  // 必填，任务ID
    "page_order": ["string"],             // 必填，页面ID顺序列表
    "current_viewing_page_id": "string",  // 必填，当前查看的页面ID
    "pages_info": [                       // 必填，页面信息列表
      {
        "page_id": "string",              // 必填，页面ID
        "status": "string",               // 必填，页面状态，可选值: "rendering", "completed", "failed", "suspended_for_human"
        "last_update": 0,                 // 必填，最后更新时间戳（毫秒）
        "render_url": "string"            // 必填，渲染后的图片URL（可为空字符串）
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
  "task_id": "string",           // 必填，任务ID
  "timestamp": 0,                // 必填，事件时间戳（毫秒）
  "viewing_page_id": "string"    // 必填，当前查看的页面ID
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

### 2.1 查询知识库

**接口路径**: `POST /api/v1/kb/query`

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "query": "string",             // 必填，查询内容
  "subject": "string",           // 可选，学科领域
  "top_k": 0,                    // 必填，返回结果数量，整数，>0
  "score_threshold": 0.0         // 可选，相似度阈值，浮点数，0.0-1.0
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "summary": "string"          // 必填，知识库检索结果摘要
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
    "subject": "数学",
    "top_k": 5,
    "score_threshold": 0.7
  }'
```

---

### 2.2 从搜索结果导入知识库

**接口路径**: `POST /api/v1/kb/ingest-from-search`

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "collection_id": "string",     // 可选，集合ID
  "items": [                     // 必填，导入项列表
    {
      "title": "string",         // 必填，标题
      "url": "string",           // 必填，来源URL
      "content": "string",       // 必填，内容
      "source": "string"         // 必填，来源标识
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
    "user_id": "user_001",
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

### 3.1 召回记忆

**接口路径**: `POST /api/v1/memory/recall`

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "session_id": "string",        // 可选，会话ID
  "query": "string",             // 必填，查询内容
  "top_k": 0                     // 必填，返回结果数量，整数，>0
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "facts": [                   // 必填，事实记忆列表
      {
        "key": "string",         // 可选，记忆键
        "content": "string",     // 可选，记忆内容
        "value": "string",       // 可选，记忆值
        "confidence": 0.0        // 可选，置信度，浮点数，0.0-1.0
      }
    ],
    "preferences": [             // 必填，偏好记忆列表
      {
        "key": "string",         // 可选，偏好键
        "content": "string",     // 可选，偏好内容
        "value": "string",       // 可选，偏好值
        "confidence": 0.0        // 可选，置信度，浮点数，0.0-1.0
      }
    ],
    "working_memory": {          // 可选，工作记忆
      "session_id": "string",    // 可选，会话ID
      "user_id": "string",       // 可选，用户ID
      "conversation_summary": "string",     // 可选，对话摘要
      "extracted_elements": {},             // 可选，提取的元素（任意JSON对象）
      "recent_topics": ["string"],          // 可选，近期话题列表
      "metadata": {                         // 可选，元数据
        "key": "value"
      }
    },
    "profile_summary": "string"  // 必填，用户画像摘要
  }
}
```

**使用示例**:

```bash
curl -X POST http://memory-service-url/api/v1/memory/recall \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "session_id": "sess_abc123",
    "query": "用户的教学风格",
    "top_k": 10
  }'
```

---

### 3.2 获取用户画像

**接口路径**: `GET /api/v1/memory/profile/{user_id}`

**请求参数**:

- `user_id` (path parameter): 必填，用户ID

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "user_id": "string",                  // 必填，用户ID
    "display_name": "string",             // 可选，显示名称
    "subject": "string",                  // 可选，学科
    "school": "string",                   // 可选，学校
    "teaching_style": "string",           // 可选，授课风格
    "content_depth": "string",            // 可选，内容深度
    "preferences": {                      // 可选，偏好设置（键值对）
      "key": "value"
    },
    "visual_preferences": {               // 可选，视觉偏好（键值对）
      "key": "value"
    },
    "history_summary": "string",          // 可选，历史摘要
    "last_active_at": 0                   // 可选，最后活跃时间戳（毫秒）
  }
}
```

**使用示例**:

```bash
curl -X GET http://memory-service-url/api/v1/memory/profile/user_001
```

---

### 3.3 提取记忆

**接口路径**: `POST /api/v1/memory/extract`

**请求参数**:

```json
{
  "user_id": "string",           // 必填，用户ID
  "session_id": "string",        // 必填，会话ID
  "messages": [                  // 必填，对话轮次列表
    {
      "role": "string",          // 必填，角色，可选值: "user", "assistant"
      "content": "string"        // 必填，内容
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
    "extracted_facts": ["string"],        // 可选，提取的事实列表
    "extracted_preferences": ["string"],  // 可选，提取的偏好列表
    "conversation_summary": "string"      // 可选，对话摘要
  }
}
```

**使用示例**:

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

### 3.4 保存工作记忆

**接口路径**: `POST /api/v1/memory/working/save`

**请求参数**:

```json
{
  "session_id": "string",        // 必填，会话ID
  "user_id": "string",           // 必填，用户ID
  "conversation_summary": "string",     // 可选，对话摘要
  "extracted_elements": {},             // 可选，提取的元素（任意JSON对象）
  "recent_topics": ["string"]           // 可选，近期话题列表
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

### 3.5 获取工作记忆

**接口路径**: `GET /api/v1/memory/working/{session_id}`

**请求参数**:

- `session_id` (path parameter): 必填，会话ID

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "session_id": "string",               // 可选，会话ID
    "user_id": "string",                  // 可选，用户ID
    "conversation_summary": "string",     // 可选，对话摘要
    "extracted_elements": {},             // 可选，提取的元素（任意JSON对象）
    "recent_topics": ["string"],          // 可选，近期话题列表
    "metadata": {"key": "value"}          // 可选，元数据
  }
}
```

**使用示例**:

```bash
curl -X GET http://memory-service-url/api/v1/memory/working/sess_abc123
```

---

## 4. 搜索服务接口

### 4.1 网络搜索

**接口路径**: `POST /api/v1/search/query`

**请求参数**:

```json
{
  "request_id": "string",        // 可选，请求ID
  "user_id": "string",           // 必填，用户ID
  "query": "string",             // 必填，搜索关键词
  "max_results": 0,              // 可选，最大结果数，整数，默认10
  "language": "string"           // 可选，语言，如 "zh-CN", "en-US"
}
```

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "request_id": "string",      // 必填，请求ID
    "status": "string",          // 必填，状态，可选值: "success", "failed", "partial"
    "results": [                 // 可选，搜索结果列表
      {
        "title": "string",       // 必填，标题
        "url": "string",         // 必填，URL
        "snippet": "string",     // 必填，摘要
        "source": "string"       // 必填，来源
      }
    ],
    "summary": "string",         // 必填，搜索结果摘要
    "duration": 0                // 可选，搜索耗时（毫秒）
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

**请求参数**:

- Content-Type: `multipart/form-data`
- 表单字段：文件数据

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "file_id": "string",         // 必填，文件ID
    "filename": "string",        // 必填，文件名
    "file_type": "string",       // 必填，文件类型
    "file_size": 0,              // 必填，文件大小（字节）
    "storage_url": "string",     // 必填，存储URL
    "purpose": "string"          // 必填，用途
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

# 第二部分：我们提供的接口

Voice Agent 对外提供以下接口供其他系统调用。

---

## WebSocket 连接接口

### 建立 WebSocket 连接

**接口路径**: `ws://voice-agent-url/ws?user_id={user_id}&session_id={session_id}`

**请求参数**:

- `user_id` (query string): 必填，用户ID
- `session_id` (query string): 可选，会话ID（如不提供则自动生成）

**连接说明**:

- 协议：WebSocket
- 支持二进制消息（音频数据）和文本消息（JSON格式）

---

### WebSocket 消息格式

#### 客户端 → 服务端消息

**1. VAD 开始**

```json
{
  "type": "vad_start"
}
```

**2. VAD 结束**

```json
{
  "type": "vad_end"
}
```

**3. 文本输入**

```json
{
  "type": "text_input",
  "text": "string"               // 必填，用户输入的文本
}
```

**4. 页面导航**

```json
{
  "type": "page_navigate",
  "task_id": "string",           // 必填，任务ID
  "page_id": "string"            // 必填，页面ID
}
```

**5. 音频数据**

- 消息类型：二进制（Binary）
- 格式：PCM 16kHz 16bit 单声道
- 每个消息包含音频数据块

---

#### 服务端 → 客户端消息

**1. 状态更新**

```json
{
  "type": "status",
  "state": "string"              // 必填，状态，可选值: "idle", "listening", "processing", "speaking"
}
```

**2. 转录文本（实时）**

```json
{
  "type": "transcript",
  "text": "string"               // 必填，实时转录文本
}
```

**3. 转录文本（最终）**

```json
{
  "type": "transcript_final",
  "text": "string"               // 必填，最终转录文本
}
```

**4. 响应文本**

```json
{
  "type": "response",
  "text": "string"               // 必填，AI响应文本（流式输出）
}
```

**5. 音频数据**

- 消息类型：二进制（Binary）
- 格式：MP3
- TTS合成的音频数据

**6. 任务状态更新**

```json
{
  "type": "task_status",
  "task_id": "string",           // 必填，任务ID
  "status": "string",            // 必填，状态
  "progress": 0,                 // 可选，进度（0-100）
  "text": "string"               // 可选，状态描述
}
```

**7. 页面渲染完成**

```json
{
  "type": "page_rendered",
  "task_id": "string",           // 必填，任务ID
  "page_id": "string",           // 必填，页面ID
  "render_url": "string",        // 必填，渲染图片URL
  "page_index": 0                // 可选，页面索引
}
```

**8. PPT 预览**

```json
{
  "type": "ppt_preview",
  "task_id": "string",           // 必填，任务ID
  "page_order": ["string"],      // 必填，页面顺序
  "pages_info": [                // 必填，页面信息列表
    {
      "page_id": "string",       // 必填，页面ID
      "status": "string",        // 必填，状态
      "last_update": 0,          // 必填，最后更新时间戳（毫秒）
      "render_url": "string"     // 必填，渲染URL
    }
  ]
}
```

**9. 导出就绪**

```json
{
  "type": "export_ready",
  "task_id": "string",           // 必填，任务ID
  "download_url": "string",      // 必填，下载URL
  "format": "string"             // 必填，格式，如 "pptx", "pdf"
}
```

**10. 冲突询问**

```json
{
  "type": "conflict_ask",
  "task_id": "string",           // 必填，任务ID
  "page_id": "string",           // 必填，页面ID
  "context_id": "string",        // 必填，上下文ID
  "question": "string"           // 必填，问题内容
}
```

**11. 需求收集进度**

```json
{
  "type": "requirements_progress",
  "status": "string",            // 必填，状态，可选值: "collecting", "confirming", "confirmed"
  "collected_fields": ["string"],// 必填，已收集字段列表
  "missing_fields": ["string"],  // 必填，缺失字段列表
  "requirements": {}             // 必填，需求对象（完整的TaskRequirements）
}
```

**12. 错误消息**

```json
{
  "type": "error",
  "code": 0,                     // 必填，错误码
  "message": "string"            // 必填，错误消息
}
```

---

## HTTP REST 接口

### 1. 文件上传（代理）

**接口路径**: `POST /api/v1/upload`

**请求参数**:

- Content-Type: `multipart/form-data`
- 表单字段：文件数据

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "file_id": "string",         // 必填，文件ID
    "filename": "string",        // 必填，文件名
    "file_type": "string",       // 必填，文件类型
    "file_size": 0,              // 必填，文件大小（字节）
    "storage_url": "string",     // 必填，存储URL
    "purpose": "string"          // 必填，用途
  }
}
```

**说明**: 此接口是透明代理，将请求转发到数据库服务的 `/api/v1/files/upload` 接口。

**使用示例**:

```bash
curl -X POST http://voice-agent-url/api/v1/upload \
  -H "Content-Type: multipart/form-data" \
  -F "file=@/path/to/document.pdf"
```

---

### 2. 获取任务预览

**接口路径**: `GET /api/v1/tasks/{task_id}/preview`

**请求参数**:

- `task_id` (path parameter): 必填，任务ID

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": "string",                  // 必填，任务ID
    "status": "string",                   // 必填，任务状态，可选值: "generating", "completed", "failed"
    "page_order": ["string"],             // 必填，页面ID顺序列表
    "current_viewing_page_id": "string",  // 必填，当前查看的页面ID
    "pages": [                            // 必填，页面信息列表
      {
        "page_id": "string",              // 必填，页面ID
        "status": "string",               // 必填，页面状态，可选值: "rendering", "completed", "failed", "suspended_for_human"
        "last_update": 0,                 // 必填，最后更新时间戳（毫秒）
        "render_url": "string"            // 必填，渲染后的图片URL（可为空字符串）
      }
    ],
    "pages_info": []                      // 必填，与pages相同（兼容字段）
  }
}
```

**状态说明**:

- 任务级 `status`:
  - `generating`: 任一页面状态为 `rendering` 或 `suspended_for_human`
  - `failed`: 任一页面状态为 `failed`
  - `completed`: 所有页面状态为 `completed`
- 页面级 `status`:
  - `rendering`: 渲染中
  - `completed`: 已完成
  - `failed`: 失败
  - `suspended_for_human`: 等待人工确认

**使用示例**:

```bash
curl -X GET http://voice-agent-url/api/v1/tasks/task_001/preview
```

---

### 3. 接收 PPT Agent 消息

**接口路径**: `POST /api/v1/voice/ppt_message`

**请求参数**:

```json
{
  "task_id": "string",           // 必填，任务ID
  "msg_type": "string",          // 可选，消息类型，默认 "tool_result"
  "priority": "string",          // 可选，优先级，可选值: "normal", "high"，默认 "normal"
  "tts_text": "string",          // 可选，TTS文本，默认 "PPT 状态已更新"
  "status": "string",            // 可选，状态（用于 ppt_status 类型）
  "progress": 0,                 // 可选，进度（0-100）
  "page_id": "string",           // 可选，页面ID
  "context_id": "string",        // 可选，上下文ID（用于冲突解决）
  "render_url": "string",        // 可选，渲染URL（用于 page_rendered 类型）
  "page_index": 0,               // 可选，页面索引
  "page_order": ["string"],      // 可选，页面顺序（用于 ppt_preview 类型）
  "pages_info": [                // 可选，页面信息列表（用于 ppt_preview 类型）
    {
      "page_id": "string",
      "status": "string",
      "last_update": 0,
      "render_url": "string"
    }
  ],
  "download_url": "string",      // 可选，下载URL（用于 export_ready 类型）
  "format": "string",            // 可选，格式（用于 export_ready 类型）
  "error_code": 0                // 可选，错误码（用于 error 类型）
}
```

**msg_type 可选值**:

- `tool_result`: 工具执行结果（默认）
- `ppt_status`: PPT状态更新
- `page_rendered`: 页面渲染完成
- `ppt_preview`: PPT预览
- `export_ready`: 导出就绪
- `conflict_question`: 冲突询问（自动设置为高优先级）
- `error`: 错误消息

**响应格式**:

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "accepted": true,            // 必填，是否接受
    "delivered": true            // 可选，是否已投递到会话（如果会话不存在则为false）
  }
}
```

**使用示例**:

1. PPT状态更新：

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

1. 页面渲染完成：

```bash
curl -X POST http://voice-agent-url/api/v1/voice/ppt_message \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "page_rendered",
    "page_id": "page_003",
    "render_url": "https://cdn.example.com/renders/page_003.png",
    "page_index": 2,
    "tts_text": "第3页已生成"
  }'
```

1. 冲突询问：

```bash
curl -X POST http://voice-agent-url/api/v1/voice/ppt_message \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "conflict_question",
    "page_id": "page_005",
    "context_id": "ctx_abc123",
    "tts_text": "第5页有两个标题，您想保留哪一个？"
  }'
```

1. 导出就绪：

```bash
curl -X POST http://voice-agent-url/api/v1/voice/ppt_message \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "export_ready",
    "download_url": "https://cdn.example.com/exports/task_001.pptx",
    "format": "pptx",
    "tts_text": "您的课件已导出完成"
  }'
```

1. 错误消息：

```bash
curl -X POST http://voice-agent-url/api/v1/voice/ppt_message \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "error",
    "error_code": 50001,
    "tts_text": "生成失败，请稍后重试"
  }'
```

---

# 附录

## A. 通用响应格式

所有HTTP接口均使用统一的响应格式：

```json
{
  "code": 0,                     // 必填，业务状态码，200表示成功
  "message": "string",           // 必填，状态消息
  "data": {}                     // 可选，响应数据
}
```

**常见状态码**:

- `200`: 成功
- `40001`: 请求参数错误
- `50000`: 服务器内部错误
- `50200`: 外部服务不可用

---

## B. TaskRequirements 完整结构

需求收集过程中的完整数据结构：

```json
{
  "session_id": "string",        // 必填，会话ID
  "user_id": "string",           // 必填，用户ID
  "topic": "string",             // 必填，课程主题
  "subject": "string",           // 必填，学科
  "knowledge_points": ["string"],// 必填，知识点列表
  "teaching_goals": ["string"],  // 必填，教学目标列表
  "teaching_logic": "string",    // 必填，讲授逻辑
  "target_audience": "string",   // 必填，目标受众
  "key_difficulties": ["string"],// 必填，重点难点列表
  "duration": "string",          // 必填，课时长度
  "total_pages": 0,              // 必填，总页数
  "global_style": "string",      // 必填，全局风格
  "interaction_design": "string",// 必填，互动设计
  "output_formats": ["string"],  // 必填，输出格式列表
  "additional_notes": "string",  // 可选，补充说明
  "reference_files": [           // 可选，参考文件列表
    {
      "file_id": "string",
      "file_url": "string",
      "file_type": "string",
      "instruction": "string"
    }
  ],
  "collected_fields": ["string"],// 必填，已收集字段列表
  "status": "string",            // 必填，状态，可选值: "collecting", "confirming", "confirmed"
  "created_at": 0,               // 必填，创建时间戳（毫秒）
  "updated_at": 0                // 必填，更新时间戳（毫秒）
}
```

---

## C. 会话状态说明

Voice Agent 会话有以下状态：

- `idle`: 空闲，等待用户输入
- `listening`: 监听中，正在接收用户语音
- `processing`: 处理中，LLM正在生成响应
- `speaking`: 播报中，TTS正在合成并播放音频

状态转换：

```
idle → listening → processing → speaking → idle
     ↑                                      ↓
     └──────────── (用户打断) ──────────────┘
```

---

## D. 优先级机制说明

Voice Agent 支持两种优先级的消息：

1. **普通优先级 (normal)**:
  - 进入普通队列
  - 在会话空闲时触发处理
  - 适用于：工具执行结果、状态更新等
2. **高优先级 (high)**:
  - 进入高优先级队列
  - 立即打断当前播报并播放
  - 被打断后会重试（最多3次）
  - 适用于：冲突询问、紧急通知

**conflict_question 类型自动设为高优先级**

---

## E. 重要注意事项

### 1. 时间戳格式

- 所有时间戳均为 Unix 毫秒时间戳（13位整数）
- 示例：`1711545600000` 表示 2024-03-27 18:00:00

### 2. 任务ID与会话关联

- 每个 `task_id` 必须关联到一个 `session_id`
- PPT Agent 推送消息时，Voice Agent 会根据 `task_id` 查找对应的会话
- 如果会话不存在或已断开，消息会被接受但不会投递

### 3. 页面ID规范

- 页面ID应保持唯一且稳定
- 建议格式：`page_{task_id}_{index}` 或使用UUID

### 4. 音频格式要求

- **输入音频**（WebSocket Binary）：PCM 16kHz 16bit 单声道
- **输出音频**（WebSocket Binary）：MP3 格式

### 5. 文件上传

- 文件上传接口 `/api/v1/upload` 是透明代理
- 实际存储由数据库服务处理
- 上传后返回的 `file_id` 和 `file_url` 用于后续引用

### 6. 冲突解决流程

- PPT Agent 发送 `conflict_question` 类型消息
- Voice Agent 播报问题并等待用户回答
- 用户回答后，Voice Agent 调用 `/api/v1/ppt/feedback` 接口
- 请求中包含 `base_timestamp` 和 `viewing_page_id` 用于版本控制

### 7. 需求收集流程

- 状态流转：`collecting` → `confirming` → `confirmed`
- `collecting`: 正在收集必填字段
- `confirming`: 所有必填字段已收集，等待用户确认
- `confirmed`: 用户确认后，调用 `/api/v1/ppt/init` 创建任务

### 8. 错误处理

- 所有接口调用失败时应返回明确的错误码和错误消息
- Voice Agent 会记录错误日志但不会重试（除高优先级消息）
- 建议实现幂等性，避免重复调用产生副作用

### 9. 并发控制

- 同一会话同时只能有一个 Pipeline 在运行
- 用户打断会取消当前 Pipeline 并启动新的
- 高优先级消息会打断当前播报

### 10. WebSocket 连接管理

- 连接断开后会自动清理会话资源
- 建议实现心跳机制保持连接活跃
- 重连时使用相同的 `session_id` 可恢复会话上下文

---

## F. 配置说明

Voice Agent 需要配置以下环境变量或配置文件：

```bash
# 服务端口
SERVER_PORT=9000

# 外部服务地址
PPT_AGENT_URL=http://ppt-agent:8080
KB_SERVICE_URL=http://kb-service:8081
MEMORY_URL=http://memory-service:8082
SEARCH_URL=http://search-service:8083
DB_SERVICE_URL=http://db-service:8084

# ASR/TTS服务
ASR_WS_URL=wss://asr-service/ws
TTS_URL=https://tts-service/api

# LLM服务
SMALL_LLM_BASE_URL=https://api.openai.com/v1
SMALL_LLM_MODEL=gpt-4o-mini
SMALL_LLM_API_KEY=sk-xxx

LARGE_LLM_BASE_URL=https://api.openai.com/v1
LARGE_LLM_MODEL=gpt-4o
LARGE_LLM_API_KEY=sk-xxx
```

---

## G. 版本信息

- **文档版本**: v1.0
- **最后更新**: 2024-03-27
- **Voice Agent 版本**: 基于代码库当前状态

---

**文档结束**