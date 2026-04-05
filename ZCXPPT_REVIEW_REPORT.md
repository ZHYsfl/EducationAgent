# zcxppt 接口实现审查报告

**审查日期**: 2026-04-05
**审查范围**: `d:\创业\myResearch\EducationAgent\zcxppt` 所有模块对照 `API_DOCUMENTATION.md` 的字段匹配、逻辑正确性、时序问题

---

## 严重等级说明

- **[CRITICAL]** 功能完全不可用 / 数据丢失
- **[HIGH]** 客户端协议破坏 / 接口字段错误 / 核心逻辑缺失
- **[MEDIUM]** 状态机错误 / 时序问题 / 竞态
- **[LOW]** 轻微不一致 / 边界情况

---

## 第一部分：POST /api/v1/ppt/init

### 1.1 Init 接口是同步阻塞的，违反异步契约

**[CRITICAL]** `internal/service/ppt_service.go:78-183` + `internal/http/handlers/ppt_handler.go:41`

`ppt_service.Init()` 在 HTTP handler goroutine 中同步执行完整流程：KB 查询 → RefFusion → LLM 生成 → 并行渲染（`wg.Wait()` 阻塞，每页最长 2 分钟）。

API 文档约定返回 `task_id` 后异步处理，客户端轮询状态。当前实现对 20 页 PPT 可能阻塞 HTTP 连接数分钟，且一旦客户端超时断开，整个生成流程也会中断。

**修复**：创建 task 后立即返回 `task_id`，将生成流程放入 `go func()` 后台执行。

---

### 1.2 `teaching_elements` 完全未传入 LLM

**[HIGH]** `internal/service/ppt_service.go:337-345`

`generateInitialPages` 构建 LLM prompt 时只用了 `topic`、`subject`、`description`、`audience`、`global_style`、`total_pages`、`kbSummary`，`req.TeachingElements`（含 `knowledge_points`、`teaching_goals`、`teaching_logic`、`key_difficulties`、`duration`、`interaction_design`、`output_formats`）完全未引用，核心教学元数据被静默丢弃。

---

### 1.3 `subject` 字段未在 API 文档中声明

**[LOW]** `internal/model/ppt.go:6`

`PPTInitRequest` 有 `subject` 字段，API 文档 §1.1 请求体中无此字段。属于未文档化扩展，建议补入文档。

---

### 1.4 `instruction` 字段在 reference_files 中被强制必填

**[LOW]** `internal/http/handlers/ppt_handler.go:51-54`

API 文档未要求 `instruction` 必填，但 handler 校验为必填，不符合规范的调用方会收到 400。

---

### 1.5 RefFusion 错误被静默吞掉，无日志

**[MEDIUM]** `internal/service/ppt_service.go:106-109`

注释写"融合失败不影响主流程，只打日志"，但实际没有任何 `log.Printf` 调用，错误完全静默。

---

### 1.6 render goroutine 的 context cancel 未 defer

**[MEDIUM]** `internal/service/ppt_service.go:144`

```go
renderCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
result, err := s.renderer.Render(renderCtx, ...)
cancel()
```

`cancel()` 未 defer，renderer panic 时 context 泄漏。应改为 `defer cancel()`。

---

## 第二部分：POST /api/v1/ppt/feedback

### 2.1 `global_modify` 与 `reorder` case 结构错误，`reorder` 不可达

**[HIGH]** `internal/service/feedback_service.go:216-247`

`global_modify` case 内部嵌入了 `reorder` 的处理逻辑，导致：
- 每次 `global_modify` 都会额外执行一次 `handleReorder`
- switch 中的 `case "reorder":` 永远不可达

这是结构性 bug，`reorder` 功能完全失效，`global_modify` 行为异常。

---

### 2.2 `base_timestamp` 从未用于版本冲突检测

**[HIGH]** `internal/service/feedback_service.go`

`base_timestamp` 被校验为 `> 0` 并传入，但在 `Handle` 中从未与页面当前版本（`UpdatedAt`/`Version`）比较。两个并发反馈可以基于同一版本提交，后者静默覆盖前者，无任何冲突提示。

---

### 2.3 冲突解决后重新处理 pending feedback 时丢失 `reference_files`

**[MEDIUM]** `internal/service/feedback_service.go:79-85, 509-515`

`PendingFeedback` 存有 `ReferenceFiles`，但重建 `FeedbackRequest` 时未映射该字段，参考文件上下文在重试时丢失。

---

### 2.4 `ProcessTimeoutTick` 无并发保护

**[MEDIUM]** `internal/service/feedback_service.go:485` + `internal/http/handlers/feedback_handler.go:73`

两个并发 tick 请求可以同时读取同一批过期 suspend，导致双重重试/双重解决。无 mutex 或原子标志保护。

---

### 2.5 LLM 调用无超时

**[LOW]** `internal/infra/llm/tool_runtime.go:98`

`RunFeedbackLoop` 中 LLM 调用只传入父 ctx，无独立 deadline。LLM 挂起时 `GeneratePages` 的 goroutine 会无限期持有 semaphore slot。

---

## 第三部分：GET /api/v1/canvas/status 与 VAD 事件

### 3.1 VAD 事件不持久化 `current_viewing_page_id`

**[HIGH]** `internal/service/ppt_service.go:485-507`

`HandleVADEvent` 验证了 `task_id`、`viewing_page_id`、`timestamp`，并处理了 suspend 解除，但从未将 `viewing_page_id` 写回 canvas 状态。`PPTRepository` 接口也没有更新 `CurrentViewingPageID` 的方法。VAD 事件的状态同步功能完全失效。

---

### 3.2 初始页面状态为 `"completed"` 而非 `"rendering"`

**[MEDIUM]** `internal/repository/ppt_repository.go:56-58`

`InitCanvas` 将所有页面初始状态设为 `"completed"`，但此时渲染尚未开始。客户端会误认为页面已就绪。应初始化为 `"rendering"`。

---

## 第四部分：GET /api/v1/tasks/{task_id}/preview

### 4.1 `/tasks/{task_id}/preview` 接口未实现

**[HIGH]** 全局

路由表中无此路由，handler 中无此实现。API 文档 §1.3 中有此接口。

---

### 4.2 `CanvasStatusResponse` 缺少 `pages` 兼容字段

**[HIGH]** `internal/model/ppt.go:41-46`

API 文档要求 `/tasks/{task_id}/preview` 响应同时包含 `pages`（主字段）和 `pages_info`（兼容字段）。当前 `CanvasStatusResponse` 只有 `pages_info`，缺少 `pages`。

---

## 第五部分：Task CRUD

### 5.1 Create 响应缺少 `status` 字段

**[HIGH]** `internal/http/handlers/task_handler.go:34`

```go
contract.Success(c, gin.H{"task_id": task.TaskID}, "success")
```

API 文档要求响应 `data` 包含 `{task_id, status}`，当前只返回 `task_id`。

---

### 5.2 `Task` 结构体缺少 `user_id` 字段

**[HIGH]** `internal/model/task.go:5-14`

`model.Task` 无 `UserID` 字段，GET `/tasks/{task_id}` 永远无法返回 `user_id`，违反 API 文档。

---

### 5.3 List 响应数组键名为 `items` 而非 `tasks`

**[HIGH]** `internal/http/handlers/task_handler.go:81`

API 文档要求 `data.tasks`，实际返回 `data.items`，客户端解析失败。

---

### 5.4 UpdateStatus 不校验合法状态值

**[MEDIUM]** `internal/service/task_service.go:33-37`

任意非空字符串均可作为 status 写入，无枚举校验。

---

## 第六部分：Export 接口

### 6.1 Export 接口是同步阻塞的，轮询无意义

**[HIGH]** `internal/service/export_service.go:33-78`

`Create` 同步执行完整导出流程，返回时 status 已是 `completed` 或 `failed`。GET 轮询接口实际上是死代码。

---

### 6.2 `expires_at` 字段完全缺失

**[HIGH]** `internal/model/export.go:14-21`

`ExportStatusResponse` 和 `ExportJob` 均无 `expires_at` 字段，API 文档要求 GET 响应包含此字段。

---

### 6.3 Create 请求要求 `format` 字段，API 文档无此字段

**[MEDIUM]** `internal/http/handlers/export_handler.go:28-29`

`format` 为必填但未在 API 文档中声明，按文档调用会收到 400。

---

## 第七部分：Notify Service

### 7.1 回调 payload 为无类型 map，无字段校验

**[MEDIUM]** `internal/service/notify_service.go:25`

`SendPPTMessage` 接受 `map[string]any`，无结构体约束，字段名拼写错误或遗漏在编译期和运行期均不报错。建议改为强类型结构体。

---

## 汇总表

| # | 等级 | 模块 | 问题 | 状态 |
|---|------|------|------|------|
| 1 | CRITICAL | `ppt_service.go` | Init 同步阻塞，违反异步契约 | 待修复 |
| 2 | HIGH | `ppt_service.go` | `teaching_elements` 未传入 LLM | 待修复 |
| 3 | HIGH | `feedback_service.go` | `global_modify`/`reorder` case 结构错误，`reorder` 不可达 | 待修复 |
| 4 | HIGH | `feedback_service.go` | `base_timestamp` 未用于冲突检测 | 待修复 |
| 5 | HIGH | `ppt_service.go` | VAD 事件不持久化 `current_viewing_page_id` | 待修复 |
| 6 | HIGH | — | `/tasks/{task_id}/preview` 接口未实现 | 待修复 |
| 7 | HIGH | `ppt.go` | `CanvasStatusResponse` 缺少 `pages` 兼容字段 | 待修复 |
| 8 | HIGH | `task_handler.go` | Create 响应缺少 `status` 字段 | 待修复 |
| 9 | HIGH | `model/task.go` | `Task` 结构体缺少 `user_id` 字段 | 待修复 |
| 10 | HIGH | `task_handler.go` | List 响应键名 `items` 应为 `tasks` | 待修复 |
| 11 | HIGH | `export_service.go` | Export 同步阻塞，轮询无意义 | 待修复 |
| 12 | HIGH | `model/export.go` | `expires_at` 字段完全缺失 | 待修复 |
| 13 | MEDIUM | `ppt_repository.go` | 初始页面状态为 `"completed"` 应为 `"rendering"` | 待修复 |
| 14 | MEDIUM | `feedback_service.go` | 冲突解决后重处理 pending 时丢失 `reference_files` | 待修复 |
| 15 | MEDIUM | `feedback_service.go` | `ProcessTimeoutTick` 无并发保护 | 待修复 |
| 16 | MEDIUM | `ppt_service.go` | render context cancel 未 defer | 待修复 |
| 17 | MEDIUM | `ppt_service.go` | RefFusion 错误静默吞掉无日志 | 待修复 |
| 18 | MEDIUM | `task_service.go` | UpdateStatus 不校验合法状态值 | 待修复 |
| 19 | MEDIUM | `export_handler.go` | `format` 字段未在 API 文档中声明 | 待修复 |
| 20 | MEDIUM | `notify_service.go` | 回调 payload 无类型约束 | 待修复 |
| 21 | LOW | `tool_runtime.go` | LLM 调用无超时 | 待修复 |
| 22 | LOW | `ppt_handler.go` | `instruction` 被强制必填，文档未要求 | 待修复 |
| 23 | LOW | `model/ppt.go` | `subject` 字段未在 API 文档中声明 | 待修复 |
