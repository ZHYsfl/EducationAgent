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
8. [搜索结果轮询接口](#8-搜索结果轮询接口)

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

## 6. 认证服务接口

### 6.1 验证用户Token

**接口路径**: `POST /api/v1/auth/verify`

**请求参数**:
```json
{
  "token": "string"              // 必填，用户token
}
```

**响应格式**:
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "user_id": "string",         // 必填，用户ID
    "valid": true                // 必填，token是否有效
  }
}
```

**使用示例**:
```bash
curl -X POST http://auth-service-url/api/v1/auth/verify \
  -H "Content-Type: application/json" \
  -d '{
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
  }'
```

---

### 6.2 获取用户基础信息

**接口路径**: `GET /api/v1/auth/profile`

**请求参数**:
- Header: `Authorization: Bearer {token}`

**响应格式**:
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "user_id": "string",         // 必填，用户ID
    "username": "string",        // 必填，用户名
    "email": "string",           // 可选，邮箱
    "display_name": "string",    // 可选，显示名称
    "avatar_url": "string",      // 可选，头像URL
    "created_at": 0              // 必填，创建时间戳（毫秒）
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
  "user_id": "string",           // 必填，用户ID
  "session_id": "string",        // 可选，会话ID（不提供则自动生成）
  "title": "string"              // 可选，会话标题
}
```

**响应格式**:
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "session_id": "string",      // 必填，会话ID
    "user_id": "string",         // 必填，用户ID
    "title": "string",           // 必填，会话标题
    "status": "string",          // 必填，状态，可选值: "active", "archived", "completed"
    "created_at": 0              // 必填，创建时间戳（毫秒）
  }
}
```

**使用示例**:
```bash
curl -X POST http://session-service-url/api/v1/sessions \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "user_001",
    "title": "高等数学课件制作"
  }'
```

---

### 7.2 获取会话详情

**接口路径**: `GET /api/v1/sessions/{session_id}`

**请求参数**:
- `session_id` (path parameter): 必填，会话ID

**响应格式**:
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "session_id": "string",      // 必填，会话ID
    "user_id": "string",         // 必填，用户ID
    "title": "string",           // 必填，会话标题
    "status": "string",          // 必填，状态，可选值: "active", "archived", "completed"
    "created_at": 0,             // 必填，创建时间戳（毫秒）
    "updated_at": 0              // 必填，更新时间戳（毫秒）
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

**请求参数**:
- `user_id` (query): 必填，用户ID
- `page` (query): 可选，页码，默认1
- `page_size` (query): 可选，每页数量，默认20

**响应格式**:
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "sessions": [
      {
        "session_id": "string",      // 必填，会话ID
        "user_id": "string",         // 必填，用户ID
        "title": "string",           // 必填，会话标题
        "status": "string",          // 必填，状态，可选值: "active", "archived", "completed"
        "created_at": 0,             // 必填，创建时间戳（毫秒）
        "updated_at": 0              // 必填，更新时间戳（毫秒）
      }
    ],
    "total": 0,                      // 必填，总数
    "page": 1                        // 必填，当前页码
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
  "title": "string",             // 可选，会话标题
  "status": "string"             // 可选，状态，可选值: "active", "archived", "completed"
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
  -d '{
    "title": "高等数学课件制作（已完成）",
    "status": "completed"
  }'
```

---

## 8. 搜索结果轮询接口

### 8.1 获取搜索结果

**接口路径**: `GET /api/v1/search/results/{request_id}`

**请求参数**:
- `request_id` (path parameter): 必填，搜索请求ID

**响应格式**:
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "request_id": "string",
    "status": "string",          // 必填，可选值: "pending", "completed", "failed"
    "results": [
      {
        "title": "string",
        "url": "string",
        "snippet": "string",
        "source": "string"
      }
    ],
    "summary": "string",
    "duration": 0
  }
}
```

**使用示例**:
```bash
curl -X GET "http://search-service-url/api/v1/search/results/req_abc123"
```

**说明**: 此接口用于轮询异步搜索任务的结果。当 `status` 为 `"pending"` 时，客户端应继续轮询；当 `status` 为 `"completed"` 或 `"failed"` 时，任务结束。

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

### 3. 接收异步服务回调

**接口路径**: `POST /api/v1/voice/callback`

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
- `search_result`: 搜索服务回调结果
- `kb_result`: 知识库查询回调结果
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
curl -X POST http://voice-agent-url/api/v1/voice/callback \
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
curl -X POST http://voice-agent-url/api/v1/voice/callback \
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
curl -X POST http://voice-agent-url/api/v1/voice/callback \
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
curl -X POST http://voice-agent-url/api/v1/voice/callback \
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
curl -X POST http://voice-agent-url/api/v1/voice/callback \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "error",
    "error_code": 50001,
    "tts_text": "生成失败，请重试"
  }'
```

6. 搜索结果回调：

```bash
curl -X POST http://voice-agent-url/api/v1/voice/callback \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "search_result",
    "tts_text": "已找到相关资料：量子力学的基本原理包括波粒二象性、不确定性原理等"
  }'
```

7. 知识库查询结果回调：

```bash
curl -X POST http://voice-agent-url/api/v1/voice/callback \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "task_001",
    "msg_type": "kb_result",
    "tts_text": "根据知识库，牛顿第二定律表述为：物体的加速度与作用力成正比，与质量成反比"
  }'
```

---
