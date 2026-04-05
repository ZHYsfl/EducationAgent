# kb-service Bug Report

> 生成时间：2026-04-05
> 对照文档：`API_DOCUMENTATION.md`
> 检查范围：`kb-service/internal/handler/`、`internal/store/`、`internal/worker/`、`internal/model/`、`internal/storage/`

---

## 目录

1. [POST /api/v1/kb/query — 查询知识库](#1-post-apiv1kbquery)
2. [POST /api/v1/kb/ingest-from-search — 从搜索结果导入](#2-post-apiv1kbingest-from-search)
3. [POST /api/v1/kb/upload — 文件上传](#3-post-apiv1kbupload)
4. [Collections & Documents 路由](#4-collections--documents-路由)
5. [Store 层（Postgres + Qdrant）](#5-store-层)
6. [Worker 层](#6-worker-层)
7. [路由 vs API 文档对照](#7-路由-vs-api-文档对照)

---

## 1. POST /api/v1/kb/query

### 1.1 响应结构与文档不符 【高危】

**文件**: `internal/model/model.go:96`

文档要求：
```json
{ "code": 200, "message": "success", "data": { "summary": "string" } }
```

实际 `KBQueryResponse` 结构：
```go
type KBQueryResponse struct {
    Chunks []RetrievedChunk `json:"chunks"`
    Total  int              `json:"total"`
}
```

返回的是原始检索分片列表，完全没有 `summary` 字段。任何按文档对接的调用方都会解析失败。

---

### 1.2 `top_k` 静默默认而非报错 【中危】

**文件**: `internal/handler/query.go:143`

```go
if req.TopK <= 0 {
    req.TopK = 5  // 静默默认
}
```

文档标注 `top_k` 为必填且 `>0`。传入 `0` 或不传时应返回参数错误，而非静默替换。另外上限 20 是未文档化的服务端约束。

---

### 1.3 `score_threshold: 0.0` 被静默覆盖 【中危】

**文件**: `internal/handler/query.go:149`

```go
if req.ScoreThreshold <= 0 {
    req.ScoreThreshold = 0.5
}
```

调用方显式传 `0.0`（表示不设阈值）会被强制改为 `0.5`，语义被篡改。同时 `>1.0` 的非法值也被接受。

---

### 1.4 BM25 稀疏向量索引错误 【高危】

**文件**: `internal/handler/query.go:68`

```go
for i, kv := range kvs {
    indices[i] = uint32(i)  // BUG: 用循环下标而非词汇表 ID
    values[i] = float32(kv.score)
}
```

每次查询的 `indices` 都是 `[0, 1, 2, ...]`，与词汇无关。混合检索的稀疏向量完全无意义，检索结果不正确。

---

### 1.5 Token 去重导致 BM25 词频恒为 1 【低危】

**文件**: `internal/handler/query.go:77`

`queryTokenize` 先去重再传给 `newQueryBM25`，导致每个词的 `tf=1`，BM25 词频权重失效。

---

### 1.6 `parseInt64` 边界问题 【低危】

**文件**: `internal/handler/query.go:268`

- 空字符串 `""` 返回 `(0, true)`，误判为有效值
- 超过 19 位数字时 `int64` 溢出，无溢出检查

---

### 1.7 下游调用无超时 【低危】

**文件**: `internal/handler/query.go:165,169,226,246`

所有下游调用（refiner、embedder、vec store、reranker）直接传 `c.Request.Context()`，未设置超时。任一下游挂起会导致 goroutine 永久阻塞。

---

## 2. POST /api/v1/kb/ingest-from-search

### 2.1 响应多余 `data` 字段 【中危】

**文件**: `internal/handler/ingest.go:120`

文档要求响应无 `data` 字段，实际返回：
```json
{ "code": 200, "message": "success", "data": { "ingested": N, "skipped": N, "doc_ids": [...] } }
```

### 2.2 必填字段 `title`/`source` 未校验 【中危】

**文件**: `internal/handler/ingest.go:66`

handler 只跳过 `URL==""` 或 `Content==""` 的 item，`title` 和 `source` 为空时静默接受并用 URL 填充 title，掩盖了缺失必填字段的问题。

### 2.3 URL 存在性检查错误被静默忽略 【中危】

**文件**: `internal/handler/ingest.go:71`

```go
exists, err := h.pg.URLExistsForUser(req.UserID, item.URL)
if err != nil {
    log.Printf(...)  // 只打日志，exists 保持 false
}
```

DB 报错时 `exists=false`，继续处理，可能导致重复导入。

### 2.4 部分失败仍返回 200 【中危】

**文件**: `internal/handler/ingest.go:97`

单条 item 的 `CreateDocumentFull` 失败时 `continue`，最终仍返回 200。全部失败时也返回 200，调用方无法感知。

### 2.5 Worker 内容哈希去重永不触发 【高危】

**文件**: `internal/worker/worker.go:196`

```go
if len(job.Content) == 64 { // 期望 SHA-256 hex
```

`ingest.go:103` 传入的是原始文本，从未预计算哈希，`len==64` 几乎不成立，内容去重完全失效。

### 2.6 DLQ drain 切片 bug 导致重复处理 【高危】

**文件**: `internal/worker/worker.go:221`

```go
remaining := append([]IndexJob{}, job)
for _, j := range jobs[1:] {
    remaining = append(remaining, j)
}
```

`job` 即 `jobs[0]`，`remaining` 等于完整的 `jobs`，已成功入队的 job 被重新推回 DLQ，造成重复处理。

### 2.7 Shutdown 后 Submit 会 panic 【高危】

**文件**: `internal/worker/worker.go:74`

`Shutdown` 关闭 `w.queue` 后，并发的 `Submit` 调用会在 `case w.queue <- job:` 处 panic（向已关闭 channel 发送）。`Submit` 未检查 `w.running`。

---

## 3. POST /api/v1/kb/upload — 文件上传

### 3.1 路由路径与文档不符 【中危】

**文件**: `internal/handler/upload.go:15`

代码注册路径为 `/api/v1/kb/upload`，文档 §5.1 要求 `/api/v1/files/upload`。

### 3.2 响应字段严重缺失 【高危】

**文件**: `internal/handler/upload.go:101`

文档要求 `data` 包含 6 个字段，实际只返回 3 个且字段名不符：

| 文档字段 | 实际字段 | 状态 |
|---|---|---|
| `file_id` | `doc_id` | 字段名错误 |
| `filename` | — | 缺失 |
| `file_type` | — | 缺失 |
| `file_size` | — | 缺失 |
| `storage_url` | `file_url` | 字段名错误 |
| `purpose` | — | 缺失 |

### 3.3 `fh.Size` 从未读取，无法返回 `file_size` 【中危】

**文件**: `internal/handler/upload.go:38`

`c.FormFile("file")` 返回的 `*multipart.FileHeader` 有 `.Size` 字段，但代码从未读取，导致 `file_size` 无法填入响应。

### 3.4 无文件大小限制 【中危】

**文件**: `internal/handler/upload.go:38`

未检查 `fh.Size`，任意大小文件直接流入存储，无拒绝机制。

### 3.5 存储成功但 DB 写入失败时文件孤立 【中危】

**文件**: `internal/handler/upload.go:64,86`

`oss.Put` 成功后若 `pg.CreateDocument` 失败，已上传的文件无清理逻辑，成为孤立对象。

### 3.6 `file_id` 使用存储路径而非独立 ID 【低危】

**文件**: `internal/handler/upload.go:79`

`FileID` 被设为 `{userID}/{docID}/{docID}{ext}`，泄露内部存储结构，且与 `file_id` 语义不符。

### 3.7 本地存储写入失败留下残缺文件 【低危】

**文件**: `internal/storage/local.go:51`

`io.Copy` 失败时 deferred `f.Close()` 关闭文件但不删除，残缺文件留在磁盘，后续 `Exists` 检查误判为已存在。

---

## 4. Collections & Documents 路由

### 4.1 ListCollectionDocuments 无集合存在性检查 【低危】

**文件**: `internal/handler/collections.go:84`

传入不存在的 `collection_id` 时静默返回空列表，而非 404。`IndexDocument` 有此检查（documents.go:53），行为不一致。

### 4.2 IndexDocument 内容哈希路径 `doc.Title` 可为空 【低危】

**文件**: `internal/handler/documents.go:91`

当 `req.Title==""` 时，内容哈希命中路径不设置 `doc.Title`，写入空字符串。fallback 路径（line 124）有 `title=req.FileID` 兜底，但此路径没有。

### 4.3 IndexDocument 双重调用 `hashed.Sum(nil)` 【低危】

**文件**: `internal/handler/documents.go:84,115`

`contentHash` 在 line 84 已正确计算，line 115 再次调用 `hex.EncodeToString(hashed.Sum(nil))` 赋给 `Content`。`Sum(nil)` 不重置 hasher，当前结果相同，但逻辑混乱，应直接复用 `contentHash` 变量。

### 4.4 DeleteDocument 响应格式不一致 【低危】

**文件**: `internal/handler/documents.go:201`

```go
c.JSON(200, gin.H{"code": 200, "message": "deleted"})
```

其他所有 handler 使用 `util.OK(c, ...)`，此处绕过统一封装，响应结构不一致。

### 4.5 UploadDocument 无去重检查 【设计缺陷】

**文件**: `internal/handler/upload.go`

`IndexDocument` 有 file_id、URL、内容哈希三重去重，`UploadDocument` 完全没有，同一文件可被重复上传索引。

---

## 5. Store 层

### 5.1 QdrantStore.sparseSearch 重复声明 — 编译错误 【致命】

**文件**: `internal/store/qdrant.go:488,590`

`sparseSearch` 方法声明了两次（line 488 为 stub，line 590 为实现），Go 编译器报 `method sparseSearch already declared for type QdrantStore`，**服务无法编译**。

### 5.2 PostgresStore DLQ 方法引用自身包名 — 编译错误 【致命】

**文件**: `internal/store/postgres.go:327,338`

```go
func (s *PostgresStore) DLQPush(job store.IndexJob) error {
```

在 `package store` 内引用 `store.IndexJob`，Go 不允许包引用自身，**编译错误**。

### 5.3 混合检索忽略 `score_threshold` 【高危】

**文件**: `internal/store/qdrant.go:359`

```go
denseChunks, err := q.vectorOnlySearch(ctx, req.Vector, searchLimit, 0, filter)
```

混合模式下 `score_threshold` 硬编码为 `0`，调用方设置的阈值完全无效，所有结果不经过滤直接返回。

### 5.4 DeleteDocument TOCTOU 竞态 【中危】

**文件**: `internal/store/postgres.go:221`

读取 `content_hash` 在事务外执行，并发删除同一文档时两个事务都读到相同 hash，导致 `DecrDocCount` 被调用两次，计数被多减。

### 5.5 CreateDocument 写入空 user_id 【中危】

**文件**: `internal/store/postgres.go:163`

`CreateDocument(d)` 调用时传 `userID=""`，`user_id` 列写入空字符串（非 NULL，PostgreSQL 不拒绝），导致所有基于 `user_id` 的查询（如 `FileExistsForUser`）失效。

### 5.6 GetDocument / ListDocumentsByCollection 不返回 user_id 【低危】

**文件**: `internal/store/postgres.go:184,260`

SELECT 语句不包含 `user_id` 列，`model.KBDocument` 也无此字段，文档所有者信息在读取后丢失。

### 5.7 DLQ 表 last_error 列永远为空 【低危】

**文件**: `internal/store/postgres.go:334`

`DLQPush` 始终写入 `last_error=""`，实际错误只打日志不持久化，DLQ 表无法用于事后排查。

---

## 6. Worker 层

### 6.1 UpsertChunks + UpdateDocumentStatus + IncrDocCount 非原子 【高危】

**文件**: `internal/worker/worker.go:185`

三步操作无事务包裹。任意步骤崩溃后系统处于不一致状态：向量已写但状态仍为 `processing`，或状态为 `indexed` 但计数未增。

### 6.2 UpdateDocumentStatus 失败被静默忽略 【中危】

**文件**: `internal/worker/worker.go:191`

```go
_ = w.meta.UpdateDocumentStatus(...)
```

状态更新失败不触发重试，文档永远停留在 `processing` 状态。

### 6.3 重试成功后 Qdrant 重复写入 【中危】

**文件**: `internal/worker/worker.go:185`

`processOnce` 无幂等保护，重试时 Qdrant upsert 重复执行，同一文档的 chunk 被多次写入，`IncrDocCount` 也被多次调用。

### 6.4 running 标志在 goroutine 启动后才设置 【低危】

**文件**: `internal/worker/worker.go:63`

goroutine 已启动，`w.running = true` 才执行。`Shutdown` 若在此窗口期调用，检查 `running==false` 提前返回，goroutine 继续运行但 channel 已关闭。

---

## 7. 路由 vs API 文档对照

### 7.1 文档覆盖的路由（均已实现）

| 文档路径 | 代码路径 | 状态 |
|---|---|---|
| `POST /api/v1/kb/query` | `main.go:117` | ✅ 已实现（但有 bug，见第1节） |
| `POST /api/v1/kb/ingest-from-search` | `main.go:118` | ✅ 已实现（但有 bug，见第2节） |

### 7.2 代码中存在但文档未覆盖的路由

这些路由是 kb-service 内部管理接口，API 文档仅描述 voice-agent 调用的两个接口，以下路由属于未文档化扩展：

| 路由 | 文件 |
|---|---|
| `POST /api/v1/kb/collections` | collections.go |
| `GET /api/v1/kb/collections` | collections.go |
| `GET /api/v1/kb/collections/:id/documents` | collections.go |
| `POST /api/v1/kb/documents` | documents.go |
| `POST /api/v1/kb/upload` | upload.go |
| `GET /api/v1/kb/documents/:id` | documents.go |
| `DELETE /api/v1/kb/documents/:id` | documents.go |
| `POST /api/v1/kb/parse` | ingest.go |
| `GET /storage/*` | main.go |
| `GET /health` | main.go |
| `GET /metrics` | main.go |

---

## 汇总：按严重程度排序

| 严重程度 | # | 位置 | 问题 |
|---|---|---|---|
| 致命（编译失败） | 5.1 | qdrant.go:488,590 | `sparseSearch` 重复声明 |
| 致命（编译失败） | 5.2 | postgres.go:327 | DLQ 方法引用自身包名 |
| 高危 | 1.1 | model.go:96 | query 响应结构完全不符文档 |
| 高危 | 1.5 | query.go:68 | BM25 稀疏向量索引无意义 |
| 高危 | 2.5 | worker.go:196 | 内容哈希去重永不触发 |
| 高危 | 2.6 | worker.go:221 | DLQ drain 切片 bug 导致重复处理 |
| 高危 | 2.7 | worker.go:74 | Shutdown 后 Submit panic |
| 高危 | 3.2 | upload.go:101 | upload 响应缺失 5 个必填字段 |
| 高危 | 5.3 | qdrant.go:359 | 混合检索忽略 score_threshold |
| 高危 | 6.1 | worker.go:185 | 三步写操作非原子 |
| 中危 | 1.2 | query.go:143 | top_k 静默默认 |
| 中危 | 1.3 | query.go:149 | score_threshold=0.0 被覆盖 |
| 中危 | 2.3 | ingest.go:71 | URL 检查 DB 错误静默忽略 |
| 中危 | 2.4 | ingest.go:97 | 部分失败返回 200 |
| 中危 | 3.3 | upload.go:38 | file_size 无法获取 |
| 中危 | 3.4 | upload.go:38 | 无文件大小限制 |
| 中危 | 3.5 | upload.go:64 | 存储成功 DB 失败文件孤立 |
| 中危 | 5.4 | postgres.go:221 | DeleteDocument TOCTOU 竞态 |
| 中危 | 5.5 | postgres.go:163 | CreateDocument 写入空 user_id |
| 中危 | 6.2 | worker.go:191 | UpdateDocumentStatus 失败静默 |
| 中危 | 6.3 | worker.go:185 | 重试导致 Qdrant 重复写入 |
| 低危 | 其余 | 各处 | 见各节详述 |


