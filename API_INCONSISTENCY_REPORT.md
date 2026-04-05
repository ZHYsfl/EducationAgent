# API 一致性检查报告

检查时间：2026-04-05  
检查范围：`auth_service`、`memory_service`  
参考文档：`API_DOCUMENTATION.md`

---

## 严重程度说明

- 🔴 严重：接口行为与文档完全不符，调用方会解析失败或功能异常
- 🟠 中等：字段缺失或校验缺失，影响部分场景
- 🟡 低：轻微不一致，不影响主流程

---

## auth_service

### 6.1 POST /api/v1/auth/verify

#### 🔴 Bug 1：接口语义与文档完全不符

**文档要求响应：**
```json
{ "code": 200, "message": "success", "data": { "user_id": "string", "valid": true } }
```

**实际响应（`auth_service.go` LoginResponse 第53-57行）：**
```json
{ "code": 200, "message": "verified", "data": { "user_id": "...", "token": "...", "expires_at": 0 } }
```

- 缺少 `valid` 字段
- 多出 `token`、`expires_at` 字段
- `message` 是 `"verified"` 而非 `"success"`
- 当前实现语义是"验证邮箱token并自动登录"，而非"验证JWT有效性"

**影响：** voice_agent 调用此接口验证用户JWT时，无法获得 `valid` 字段，逻辑会出错。

---

#### 🔴 Bug 2：JWT exp claim 存毫秒时间戳（违反标准）

**位置：** `token_manager.go` 第27-29行

```go
claims := jwt.MapClaims{
    "exp": expiresAt,  // expiresAt 是毫秒时间戳
}
```

JWT 标准要求 `exp` 为秒级 Unix 时间戳，但此处存的是毫秒值（约为正常值的1000倍）。

- `golang-jwt/jwt` 库内置的过期检查永远不会触发
- 代码用手动毫秒比较（第69-71行）弥补，本服务内能工作
- 但任何其他服务用标准 JWT 库解析此 token，过期校验完全失效

---

### 6.2 GET /api/v1/auth/profile

#### 🟠 Bug 3：响应缺少 avatar_url 字段

**文档要求：** `avatar_url: "string"（可选）`

**实际：** `ProfileResponse`（第59-68行）、`model.User`、数据库 migration 均无 `avatar_url` 字段，响应中该字段不存在。

---

## memory_service

### 3.1 POST /api/v1/memory/recall

#### 🔴 Bug 4：facts/preferences 每条记录缺少 content 字段

**文档要求每条记录：** `{ "key", "content", "value", "confidence" }`

**实际 MemoryEntry 结构体（`model/memory_entry.go`）：**

| 文档字段 | 实际字段 | 状态 |
|---------|---------|------|
| key | key | ✅ |
| content | ❌ 无此字段（有 context，json tag 为 "context"） | 缺失 |
| value | value | ✅ |
| confidence | confidence | ✅ |

调用方按文档解析 `content` 字段会得到空值。

---

#### 🟠 Bug 5：top_k <= 0 时静默 fallback 而非报错

**文档要求：** `top_k` 必填，整数 `>0`

**实际（`memory_service.go` 第169-172行）：**
```go
topK := req.TopK
if topK <= 0 {
    topK = 10  // 静默 fallback
}
```

传 0 或负数不报错，与文档"必填且>0"不符。

---

#### 🟠 Bug 6：working_memory 缺少 metadata 字段

**文档要求 working_memory 包含：** `metadata: {}`

**实际 WorkingMemory 结构体（`model/working_memory.go`）：** 无 `Metadata` 字段，响应中不出现该字段。

同样影响：`GET /api/v1/memory/working/{session_id}` 响应也缺少 `metadata`。

---

### 3.2 GET /api/v1/memory/profile/{user_id}

#### 🟠 Bug 7：content_depth 字段无法写入，永远返回空

**位置：** `memory_service.go` UpdateProfileRequest（第69-75行）缺少 `ContentDepth` 字段，也无对应 upsert 逻辑。

`GetProfile` 第265-276行的 switch 有 `case "content_depth"` 分支，但数据库中永远不会有该 key 的记录，导致 `content_depth` 永远返回空字符串。

---

#### 🟡 Bug 8：history_summary 取值依赖 DB 返回顺序

**位置：** `memory_service.go` 第259-261行

```go
if e.Category == "summary" && profile.HistorySummary == "" {
    profile.HistorySummary = e.Value
}
```

多条 summary 时取第一条，未按时间取最新，结果不确定。

---

#### 🟡 Bug 9：user 不存在时错误码语义不准

**位置：** `memory_service.go` 第236-240行

user 不存在返回 `CodeInvalidCredentials`（40100），语义是"凭证无效"，应为 `CodeResourceNotFound`（40400）。

---

### 3.3 POST /api/v1/memory/extract

#### 🔴 Bug 10：extracted_facts/preferences 返回完整对象而非字符串数组

**文档要求：**
```json
{ "extracted_facts": ["string"], "extracted_preferences": ["string"] }
```

**实际（`memory_service.go` 第36-41行）：**
```go
type MemoryExtractResponse struct {
    ExtractedFacts       []model.MemoryEntry `json:"extracted_facts"`
    ExtractedPreferences []model.MemoryEntry `json:"extracted_preferences"`
    ...
}
```

返回完整 MemoryEntry 对象数组（含 memory_id、category 等字段），调用方按文档解析字符串数组会失败。

---

#### 🟠 Bug 11：session_id 文档标注必填但代码不校验

**文档：** `session_id` 必填

**实际（`memory_service.go` 第86行）：**
```go
if strings.TrimSpace(req.UserID) == "" || len(req.Messages) == 0 {
```

`session_id` 未做必填校验，传空不报错，只是跳过 working memory 写入。

---

#### 🟠 Bug 12：messages[].role 无枚举校验

**文档：** role 可选值为 `"user"` 或 `"assistant"`

**实际：** `ConversationTurn.Role` 无任何校验，传入任意字符串（如 `"system"`、`""`）被静默接受并传给 extractor。

---

### 3.4 POST /api/v1/memory/working/save

#### 🔴 Bug 13：extracted_elements 是固定结构体，不支持任意 JSON 对象

**文档要求：** `extracted_elements: {}` 支持任意 JSON 对象

**实际（`memory_service.go` 第82行）：**
```go
ExtractedElements model.TeachingElements `json:"extracted_elements"`
```

`TeachingElements` 只有6个固定字段（knowledge_points、teaching_goals、key_difficulties、target_audience、duration、output_style），传入其他字段静默丢弃。

---

## 汇总

| # | 严重程度 | 服务 | 接口 | 问题 |
|---|---------|------|------|------|
| 1 | 🔴 严重 | auth_service | POST /auth/verify | 接口语义完全不符，返回 LoginResponse 而非 {user_id, valid} |
| 2 | 🔴 严重 | auth_service | — | JWT exp 存毫秒，跨服务过期校验失效 |
| 3 | 🟠 中等 | auth_service | GET /auth/profile | 缺少 avatar_url 字段 |
| 4 | 🔴 严重 | memory_service | POST /memory/recall | facts/preferences 缺少 content 字段 |
| 5 | 🟠 中等 | memory_service | POST /memory/recall | top_k<=0 静默 fallback 而非报错 |
| 6 | 🟠 中等 | memory_service | POST /memory/recall & GET /memory/working | working_memory 缺少 metadata 字段 |
| 7 | 🟠 中等 | memory_service | GET /memory/profile | content_depth 无法写入，永远返回空 |
| 8 | 🟡 低 | memory_service | GET /memory/profile | history_summary 取值依赖 DB 顺序 |
| 9 | 🟡 低 | memory_service | GET /memory/profile | user 不存在返回 40100 而非 40400 |
| 10 | 🔴 严重 | memory_service | POST /memory/extract | extracted_facts/preferences 返回对象数组而非字符串数组 |
| 11 | 🟠 中等 | memory_service | POST /memory/extract | session_id 文档必填但代码不校验 |
| 12 | 🟠 中等 | memory_service | POST /memory/extract | messages[].role 无枚举校验 |
| 13 | 🔴 严重 | memory_service | POST /memory/working/save | extracted_elements 固定结构体，不支持任意 JSON |
