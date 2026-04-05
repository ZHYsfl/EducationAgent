# Interface 模块接口 Bug 报告

> 检查时间: 2026-04-05
> 检查范围: `d:/创业/myResearch/EducationAgent/interface/internal/server/`
> 对照文档: `API_DOCUMENTATION.md`

---

## 严重程度说明

- **critical**: 安全漏洞或接口完全失效，必须立即修复
- **major**: 功能错误或性能问题，影响正常使用
- **minor**: 轻微不一致或边缘问题

---

## §6 认证接口 (auth_handler / handlers.go)

### [critical] Bug A1 — token 格式假设与文档不符，JWT 永远验证失败

**位置**: `common.go: parseUserID`

**问题**: `parseUserID` 要求 token 去掉 `Bearer ` 后必须以 `user_` 开头才返回有效 userID。文档 §6.1 示例 token 是 JWT（`eyJ...`），客户端按文档传 JWT 时 `valid` 永远是 `false`，`authProfile` 永远返回 401。

**修复**: 实现真正的 JWT 解析（取 `sub` 或 `user_id` claim），或在文档中明确 token 格式约束并同步客户端。

---

### [major] Bug A2 — authVerify 重复拼接 "Bearer "

**位置**: `handlers.go: authVerify`

```go
// 当前（脆弱巧合）
userID := parseUserID("Bearer " + token)

// 修复：拆分函数，裸 token 走独立路径
userID := parseRawToken(token)
```

`req.Token` 已是裸 token，再拼 `"Bearer "` 依赖 `parseUserID` 内部剥离，是脆弱的巧合。

---

### [major] Bug A3 — authProfile 的 created_at 每次返回当前时间

**位置**: `handlers.go: authProfile`

```go
now := nowMs()  // 每次调用都是当前时间，不是注册时间
ok(c, gin.H{"created_at": now, ...})
```

文档 §6.2 `created_at` 语义是用户注册时间，应从存储层读取。

---

### [major] Bug A4 — 鉴权仅校验格式，无签名/过期验证，可伪造身份

**位置**: `common.go: parseUserID`, `handlers.go: authProfile`

任何人构造 `Bearer user_xxx` 即可通过鉴权，拿到任意 user_id 的 profile。需补充签名校验和过期检查。

---

## §5 文件接口 (handlers.go)

### [critical] Bug F1 — getFile 无归属鉴权，越权读取他人文件

**位置**: `handlers.go: getFile`

只校验 `file_id` 格式，未验证文件是否属于当前 token 用户。

```go
// 修复：查询时加 user_id 条件
userID := userIDFromContext(c)
a.db.First(&rec, "id = ? AND user_id = ?", fileID, userID)
// 统一返回 404 避免枚举
```

---

### [critical] Bug F2 — deleteFile 无归属鉴权，越权删除他人文件

**位置**: `handlers.go: deleteFile`

同 F1，任何已登录用户可删除他人文件。修复方式同上。

---

### [major] Bug F3 — OSS 上传成功但 DB 写入失败时对象泄漏

**位置**: `handlers.go: uploadFile`

`storage.Upload` 成功后 `db.Create` 失败，OSS 对象永久孤立无法清理。

```go
if err = a.db.Create(&rec).Error; err != nil {
    go func() { _ = a.storage.Delete(context.Background(), objectKey) }()
    fail(c, 50000, "保存 files 元数据失败")
    return
}
```

---

### [major] Bug F4 — 无文件大小限制，OOM 风险

**位置**: `handlers.go: uploadFile`

`FormFile` 无大小限制，超大文件耗尽内存。

```go
// 路由初始化时
router.MaxMultipartMemory = 32 << 20  // 32MB
// 或 handler 开头
c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 100<<20)
```

---

### [major] Bug F5 — getFile 每次全量下载做 checksum，大文件性能灾难

**位置**: `handlers.go: getFile`

每次 GET 都从 OSS 下载完整文件流计算 SHA256，对视频等大文件造成秒级延迟和大量带宽消耗。checksum 在 upload 时已存入 DB，直接返回 `rec.Checksum` 即可，删除全量下载校验逻辑。

---

### [minor] Bug F6 — Rollback 错误被静默丢弃

**位置**: `handlers.go: deleteFile`

`tx.Rollback()` 失败时错误被忽略，建议至少记录日志。

---

## §7 会话接口 (handlers.go)

### [critical] Bug S1 — createSession 的 user_id 可被客户端伪造

**位置**: `handlers.go: createSession`

```go
// 当前（有问题）：请求体 user_id 优先于 token
if req.UserID == "" { req.UserID = userIDFromContext(c) }

// 修复：始终以 token 为准
req.UserID = userIDFromContext(c)
```

攻击者可在请求体填入他人 `user_id` 以其身份创建会话。

---

### [critical] Bug S2 — createSession TOCTOU 竞态，重复 session_id 可绕过检查

**位置**: `handlers.go: createSession`

`First` 检查和 `Create` 插入非原子，并发请求可同时通过检查。

**修复**: 在 `sessions.id` 加唯一索引，捕获重复键错误而非先查后插：

```go
if err := a.db.Create(&rec).Error; err != nil {
    if isDuplicateKeyError(err) {
        fail(c, 40900, "会话已存在"); return
    }
    fail(c, 50000, "创建会话失败"); return
}
```

---

### [critical] Bug S3 — getSession/listSessions 未校验归属，越权读取他人会话

**位置**: `handlers.go: getSession, listSessions`

`getSession` 不验证会话是否属于当前用户；`listSessions` 的 `user_id` 来自 query 参数，可传入他人 ID。

```go
// getSession 修复
if rec.UserID != userIDFromContext(c) {
    fail(c, 40300, "无权访问"); return
}
// listSessions 修复：强制使用 token 中的 userID
userID := userIDFromContext(c)
```

---

### [major] Bug S4 — getSession/updateSession 路由参数名不匹配，接口完全失效

**位置**: `internal/server/router.go` vs `handlers.go`

路由定义为 `/:session_id`（实际需确认），但 `c.Param("session_id")` 若与路由占位符名称不一致则永远返回空字符串，导致格式校验直接报 40001。

**修复**: 确保路由定义中的参数名与 `c.Param()` 调用一致。

---

### [major] Bug S5 — updateSession 并发 Lost Update

**位置**: `handlers.go: updateSession`

先 `First` 读再 `Updates` 写，并发时后写覆盖先写。

```go
// 修复：只更新客户端传入的字段，去掉先读
updates := map[string]interface{}{"updated_at": nowMs()}
if req.Title != "" { updates["title"] = req.Title }
if req.Status != "" { updates["status"] = req.Status }
result := a.db.Model(&SessionModel{}).Where("id = ?", sid).Updates(updates)
if result.RowsAffected == 0 { fail(c, 40400, "会话不存在"); return }
```

---

### [major] Bug S6 — updateSession title 无法被清空

**位置**: `handlers.go: updateSession`

`req.Title == ""` 时跳过更新，客户端无法将 title 置为空字符串。

**修复**: 使用指针区分"未传"与"传了空值"：

```go
var req struct {
    Title  *string `json:"title"`
    Status *string `json:"status"`
}
```

---

### [minor] Bug S7 — listSessions count/find 非原子，total 可能不一致

**位置**: `handlers.go: listSessions`

两次独立查询之间若有插入/删除，`total` 与实际 `rows` 数量不一致。可接受轻微不一致，或在事务中执行两次查询。

---

## §4.1 / §8.1 搜索接口 (handlers.go + search_service.go)

### [critical] Bug Q1 — searchQuery TOCTOU 竞态，重复 request_id 可绕过检查

**位置**: `handlers.go: searchQuery`

同 S2，`Where+First` 检查和 `Create` 非原子。

**修复**: 在 `search_requests.request_id` 加唯一索引，捕获重复键错误。

---

### [critical] Bug Q2 — searchQuery 的 user_id 未与 token 校验，可伪造身份

**位置**: `handlers.go: searchQuery`

`user_id` 完全来自请求体，任何持有有效 token 的用户可以他人身份发起搜索。

```go
// 修复
authedUserID := userIDFromContext(c)
if authedUserID != req.UserID {
    fail(c, 40301, "user_id 与认证身份不符"); return
}
```

---

### [critical] Bug Q3 — searchResult 无鉴权，任意用户可查他人搜索结果

**位置**: `handlers.go: searchResult`

只校验 `request_id` 格式，不校验归属。

```go
// 修复：查出记录后校验
if rec.UserID != userIDFromContext(c) {
    fail(c, 40301, "无权访问该搜索记录"); return
}
```

---

### [major] Bug Q4 — NormalizeStoredStatusForSection8 default 分支语义错误

**位置**: `search_service.go: NormalizeStoredStatusForSection8`

```go
default: return "completed"  // 错误：把 "processing" 等中间态映射为 completed
```

客户端会误认为任务已完成。

```go
default: return "pending"  // 修复：未知状态保守处理为仍在进行
```

---

### [major] Bug Q5 — goroutine 可能不响应 ctx 取消，泄漏风险

**位置**: `search_service.go: runSearchJob`

`runSearchPipeline` 内部子调用若未正确传递 `ctx`，超时后子 goroutine 继续运行。需确保所有子调用都监听 `ctx.Done()`。

---

### [minor] Bug Q6 — searchResult JSON unmarshal 失败静默忽略

**位置**: `handlers.go: searchResult`

`json.Unmarshal` 失败时 `results` 返回空数组，数据损坏被掩盖。建议记录错误日志。

---

### [minor] Bug Q7 — language 默认值 "zh" 与文档示例 "zh-CN" 不一致

**位置**: `handlers.go: searchQuery`

文档示例用 `"zh-CN"`，实现默认 `"zh"`，可能影响下游搜索引擎语言匹配。建议与文档和下游服务对齐。

---

## 汇总

| ID | 接口 | 严重程度 | 问题摘要 |
|----|------|----------|----------|
| A1 | Auth §6.1/6.2 | critical | JWT token 永远验证失败 |
| A2 | Auth §6.1 | major | 重复拼接 Bearer 是脆弱巧合 |
| A3 | Auth §6.2 | major | created_at 返回当前时间非注册时间 |
| A4 | Auth §6.2 | major | 鉴权无签名/过期验证，可伪造 |
| F1 | File GET | critical | 无归属鉴权，越权读取他人文件 |
| F2 | File DELETE | critical | 无归属鉴权，越权删除他人文件 |
| F3 | File POST | major | OSS 上传成功 DB 失败时对象泄漏 |
| F4 | File POST | major | 无文件大小限制，OOM 风险 |
| F5 | File GET | major | 全量下载做 checksum，大文件性能灾难 |
| F6 | File DELETE | minor | Rollback 错误静默丢弃 |
| S1 | Session POST | critical | user_id 可被客户端伪造 |
| S2 | Session POST | critical | TOCTOU 竞态，重复 session_id 可绕过 |
| S3 | Session GET/LIST | critical | 无归属鉴权，越权读取他人会话 |
| S4 | Session GET/PUT | major | 路由参数名不匹配，接口可能完全失效 |
| S5 | Session PUT | major | 并发 Lost Update |
| S6 | Session PUT | major | title 无法被清空 |
| S7 | Session LIST | minor | count/find 非原子，total 可能不一致 |
| Q1 | Search POST | critical | TOCTOU 竞态，重复 request_id 可绕过 |
| Q2 | Search POST | critical | user_id 未与 token 校验，可伪造身份 |
| Q3 | Search GET | critical | 无归属鉴权，越权查看他人搜索结果 |
| Q4 | Search GET | major | default→"completed" 掩盖中间态 |
| Q5 | Search async | major | goroutine 可能不响应 ctx 取消 |
| Q6 | Search GET | minor | unmarshal 失败静默忽略 |
| Q7 | Search POST | minor | language 默认值与文档不一致 |

**critical: 9 个 / major: 11 个 / minor: 4 个**
