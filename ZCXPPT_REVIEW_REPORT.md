# zcxppt 接口实现审查报告

**审查日期**: 2026-04-05
**审查范围**: `zcxppt` 所有模块，对照 `API_DOCUMENTATION.md` 及 `voice_agent` 实际调用代码双重验证

---

## 严重等级说明

- **[CRITICAL]** 功能完全不可用 / 数据丢失
- **[HIGH]** 核心链路破坏 / 字段错误
- **[MEDIUM]** 状态机错误 / 竞态 / 时序问题
- **[LOW]** 轻微不一致 / 代码质量

> 标注 `📌 文档错误` 的条目：问题出在 API 文档描述有误，zcxppt 实现本身无需修改，应更新文档。

---

## 第一部分：POST /api/v1/ppt/init

### 1.1 Init 接口同步阻塞，违反异步契约

**[CRITICAL]** `internal/service/ppt_service.go:78-183` + `internal/http/handlers/ppt_handler.go:41`

`ppt_service.Init()` 在 HTTP handler goroutine 中同步执行完整流程：KB 查询 → RefFusion → LLM 生成 → 并行渲染（`wg.Wait()` 阻塞，每页最长 2 分钟）。

voice_agent 调用 `InitPPT` 后等待响应，20 页 PPT 可能阻塞数分钟，HTTP 超时后生成流程也会中断。

**修复**：创建 task 后立即返回 `task_id`，将生成流程放入 `go func()` 后台执行。

---

### 1.2 `teaching_elements` 完全未传入 LLM

**[HIGH]** `internal/service/ppt_service.go:337-345`

voice_agent 在 `PPTInitRequest` 中填充了完整的 `teaching_elements`（`knowledge_points`、`teaching_goals`、`teaching_logic`、`key_difficulties`、`duration`、`interaction_design`、`output_formats`），但 `generateInitialPages` 构建 LLM prompt 时完全未引用 `req.TeachingElements`，核心教学元数据被静默丢弃。

---

### 1.3 `subject` 字段未在 API 文档中声明 📌 文档错误

**[LOW]** `internal/model/ppt.go:6`

`PPTInitRequest` 有 `subject` 字段，voice_agent 不发送该字段（其 `PPTInitRequest` 无此字段），zcxppt 内部用于 KB 查询。建议在 API 文档 §1.1 中补充此字段说明。

---

### 1.4 `instruction` 在 reference_files 中被强制必填

**[LOW]** `internal/http/handlers/ppt_handler.go:51-54`

API 文档未要求 `instruction` 必填，但 handler 校验为必填。voice_agent 发送时填充了该字段，当前不影响链路，但文档应补充此约束。

---

### 1.5 RefFusion 错误静默吞掉，无日志

**[MEDIUM]** `internal/service/ppt_service.go:106-109`

注释写"只打日志"，但无任何 `log.Printf` 调用，错误完全静默，排查困难。

---

### 1.6 render goroutine 的 context cancel 未 defer

**[MEDIUM]** `internal/service/ppt_service.go:144`

`cancel()` 未 defer，renderer panic 时 context 泄漏。应改为 `defer cancel()`。

---

## 第二部分：POST /api/v1/ppt/feedback

### 2.1 `global_modify` 与 `reorder` case 结构错误，`reorder` 不可达

**[HIGH]** `internal/service/feedback_service.go:216-247`

`global_modify` case 内部嵌入了 `reorder` 的处理逻辑，导致：
- 每次 `global_modify` 都额外执行一次 `handleReorder`
- switch 中的 `case "reorder":` 永远不可达

`reorder` 功能完全失效，`global_modify` 行为异常。

---

### 2.2 `base_timestamp` 未用于乐观锁冲突检测 📌 文档错误

**[LOW]** `internal/service/feedback_service.go`

API 文档描述 `base_timestamp` 用于"版本控制"，但 zcxppt 实际将其用于 suspend 流程的时间基准，不做乐观锁比较。这是设计选择，文档描述夸大了其用途，应更新文档说明实际语义。

---

### 2.3 冲突解决后重处理 pending feedback 时丢失 `reference_files`

**[LOW]** `internal/service/feedback_service.go:79-85, 509-515`

`PendingFeedback` 存有 `ReferenceFiles`，重建 `FeedbackRequest` 时未映射。voice_agent 的 feedback 请求不带 reference_files，当前不影响主链路，但若未来支持带文件反馈则会有问题。

---

### 2.4 `ProcessTimeoutTick` 无并发保护

**[MEDIUM]** `internal/service/feedback_service.go:485` + `internal/http/handlers/feedback_handler.go:73`

两个并发 tick 请求可同时读取同一批过期 suspend，导致双重重试/双重解决。

---

### 2.5 LLM 调用无超时

**[LOW]** `internal/infra/llm/tool_runtime.go:98`

`RunFeedbackLoop` 中 LLM 调用无独立 deadline，LLM 挂起时 goroutine 无限期持有 semaphore slot。

---

## 第三部分：GET /api/v1/canvas/status 与 VAD 事件

### 3.1 VAD 事件不持久化 `current_viewing_page_id`

**[HIGH]** `internal/service/ppt_service.go:485-507`

voice_agent 发送 VAD 事件时携带 `viewing_page_id`，`HandleVADEvent` 验证后处理了 suspend 解除，但从未将 `viewing_page_id` 写回 canvas 的 `CurrentViewingPageID`。`PPTRepository` 接口也无更新该字段的方法。

后续 canvas status 返回的 `current_viewing_page_id` 永远是初始值，voice_agent 无法获知用户当前查看的页面。

---

### 3.2 初始页面状态为 `"completed"` 而非 `"rendering"`

**[MEDIUM]** `internal/repository/ppt_repository.go:56-58`

`InitCanvas` 将所有页面初始状态设为 `"completed"`，但渲染尚未开始。voice_agent 读取 canvas status 时会误认为页面已就绪，可能提前展示空白页面。应初始化为 `"rendering"`。

---

## 第四部分：/tasks/{task_id}/preview 接口 📌 文档错误

### 4.1 该接口由 voice_agent 对外提供，不是 zcxppt 的接口

**[LOW]** 📌 文档错误

API 文档 §1.3 将 `GET /api/v1/tasks/{task_id}/preview` 列为 zcxppt 提供的接口，但实际上：
- voice_agent 调用的是 zcxppt 的 `GET /api/v1/canvas/status`
- `GET /api/v1/tasks/{task_id}/preview` 是 voice_agent 对前端暴露的接口（`agent/http.go:HandlePreview`）

zcxppt 无需实现此接口。API 文档中该接口的归属描述有误，应移至"voice_agent 提供的接口"章节。

---

## 第五部分：Task CRUD 接口 📌 文档错误

### 5.1 voice_agent 不调用任何 Task CRUD 接口

**[LOW]** 📌 文档错误

voice_agent 的 `ExternalServices` 接口只定义了 `InitPPT`、`SendFeedback`、`GetCanvasStatus`、`NotifyVADEvent`，完全不调用 `/api/v1/tasks` 系列接口。

Task CRUD 接口（Create/Get/List/UpdateStatus）是 zcxppt 内部或其他调用方使用的接口，不在 voice_agent ↔ zcxppt 链路上。以下问题不影响当前链路，但作为接口质量问题记录：

- `task_handler.go:34`：Create 响应缺少 `status` 字段
- `model/task.go`：`Task` 结构体缺少 `user_id` 字段
- `task_handler.go:81`：List 响应键名 `items` 应为 `tasks`
- `task_service.go:33-37`：UpdateStatus 不校验合法状态值

---

## 第六部分：Export 接口 📌 部分文档错误

### 6.1 Export 接口同步阻塞，轮询无意义

**[MEDIUM]** `internal/service/export_service.go:33-78`

`Create` 同步执行完整导出流程，返回时 status 已是 `completed` 或 `failed`，GET 轮询接口是死代码。

voice_agent 不调用 export API（只接收 `export_ready` 回调），但若未来有其他调用方需要轮询，此问题会暴露。

---

### 6.2 `expires_at` 字段缺失，但 voice_agent 不使用

**[LOW]** 📌 文档错误

`ExportStatusResponse` 无 `expires_at` 字段。voice_agent 接收 `export_ready` 回调时只用 `download_url` 和 `format`，不用 `expires_at`。API 文档中该字段可标注为可选或移除。

---

### 6.3 Create 请求要求 `format` 字段，API 文档无此字段

**[LOW]** 📌 文档错误

`format` 为必填但未在 API 文档中声明。voice_agent 不调用 export，不受影响。建议补入文档。

---

## 第七部分：Notify Service

### 7.1 回调 payload 为无类型 map

**[LOW]** `internal/service/notify_service.go:25`

`SendPPTMessage` 接受 `map[string]any`，字段名拼写错误在编译期不报错。功能上已正确，建议改为强类型结构体提升可维护性。

---

## 汇总表

| # | 等级 | 模块 | 问题 | 影响链路 | 状态 |
|---|------|------|------|----------|------|
| 1 | CRITICAL | `ppt_service.go` | Init 同步阻塞 | ✅ 是 | 待修复 |
| 2 | HIGH | `ppt_service.go` | `teaching_elements` 未传入 LLM | ✅ 是 | 待修复 |
| 3 | HIGH | `feedback_service.go` | `global_modify`/`reorder` 结构错误 | ✅ 是 | 待修复 |
| 4 | HIGH | `ppt_service.go` | VAD 不持久化 `current_viewing_page_id` | ✅ 是 | 待修复 |
| 5 | MEDIUM | `ppt_repository.go` | 初始页面状态为 `"completed"` | ✅ 是 | 待修复 |
| 6 | MEDIUM | `feedback_service.go` | `ProcessTimeoutTick` 无并发保护 | ✅ 是 | 待修复 |
| 7 | MEDIUM | `ppt_service.go` | RefFusion 错误静默无日志 | 间接 | 待修复 |
| 8 | MEDIUM | `ppt_service.go` | render context cancel 未 defer | 间接 | 待修复 |
| 9 | MEDIUM | `export_service.go` | Export 同步阻塞 | 否 | 待修复 |
| 10 | LOW | `feedback_service.go` | `base_timestamp` 语义与文档不符 | 📌 文档错误 | 更新文档 |
| 11 | LOW | — | `/tasks/{task_id}/preview` 归属错误 | 📌 文档错误 | 更新文档 |
| 12 | LOW | — | Task CRUD 字段问题（3处） | 否 | 待修复 |
| 13 | LOW | `model/export.go` | `expires_at` 缺失 | 📌 文档错误 | 更新文档 |
| 14 | LOW | `export_handler.go` | `format` 未在文档声明 | 📌 文档错误 | 更新文档 |
| 15 | LOW | `model/ppt.go` | `subject` 未在文档声明 | 📌 文档错误 | 更新文档 |
| 16 | LOW | `notify_service.go` | 回调 payload 无类型约束 | 否 | 待修复 |
| 17 | LOW | `tool_runtime.go` | LLM 调用无超时 | 间接 | 待修复 |
| 18 | LOW | `feedback_service.go` | pending 重处理丢失 `reference_files` | 否 | 待修复 |
| 19 | LOW | `ppt_handler.go` | `instruction` 强制必填文档未说明 | 📌 文档错误 | 更新文档 |
