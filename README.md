## 修复说明（曾晨曦 / `zcxppt`，2026-04-05）

对照 `API_DOCUMENTATION.md`（init 响应含 `processing`）、架构图中 voice_agent 轮询 canvas 的异步预期，审查报告中的问题经代码核对均成立，已按项修复。未将「问题误判为正确」而保留的项。

### 1. Init 异步（CRITICAL）

- **问题核实**：`Init` 内联 KB、LLM、多路渲染与 `wg.Wait()`，HTTP 长时间占用，与文档中 `status: processing` 及调用方超时风险一致。
- **修改**：`Init` 在创建 task 与 `InitCanvas` 后，若 `canGenerateInitWithKB()` 为真则 `go runInitGeneration(req, taskID)`，立即返回 `(task_id, "processing", nil)`；生成逻辑迁至 `runInitGeneration`，失败时 `UpdateStatus(failed)`，成功 `completed`。未配置 KB/LLM 时仍同步标记 `completed` 并返回 `"completed"`。
- **文件**：`internal/service/ppt_service.go`，`internal/http/handlers/ppt_handler.go`（响应 `status` 使用服务返回值）。

### 2. `teaching_elements` 传入 LLM（HIGH）

- **问题核实**：`generateInitialPages` 的 user prompt 仅含 topic/description 等，未含 `TeachingElements`。
- **修改**：新增 `formatTeachingElementsForPrompt`，将 `req.TeachingElements` 序列化为 JSON 拼入 prompt；缺失时给出明确占位说明。
- **文件**：`internal/service/ppt_service.go`。

### 3. `global_modify` / `reorder` 分支（HIGH）

- **问题核实**：`reorder` 逻辑缩进落在 `global_modify` 的 `switch` 分支内，`case "reorder"` 不可达，且每次 `global_modify` 会误跑 `handleReorder`。
- **修改**：将 `global_modify` 的 for 循环体写完整；独立 `case "reorder":` 仅调用 `handleReorder`。
- **文件**：`internal/service/feedback_service.go`。

### 4. VAD 持久化 `current_viewing_page_id`（HIGH）

- **问题核实**：`HandleVADEvent` 校验页面后未更新画布。
- **修改**：`PPTRepository` 增加 `SetCurrentViewingPageID(taskID, pageID)`；内存与 Redis 实现均校验 `pageID` 属于 `PageOrder` 后写回 `CurrentViewingPageID`；VAD 处理中在 `GetPageRender` 成功后调用。
- **文件**：`internal/repository/ppt_repository.go`，`internal/repository/ppt_redis_repository.go`，`internal/service/ppt_service.go`。

### 5. 初始页面状态 `rendering`（MEDIUM）

- **问题核实**：`InitCanvas` 中页面与 `PagesInfo` 均为 `completed`，与尚未渲染的事实不符。
- **修改**：初始 `Status` 改为 `"rendering"`；`UpdatePageCode` 仍置为 `completed`。
- **文件**：`internal/repository/ppt_repository.go`，`internal/repository/ppt_redis_repository.go`。

### 6. `ProcessTimeoutTick` 并发（MEDIUM）

- **问题核实**：并发 tick 可能重复处理同一批过期 suspend。
- **修改**：在 `FeedbackService` 增加 `timeoutTickMu`，`ProcessTimeoutTick` 整段加锁。
- **文件**：`internal/service/feedback_service.go`。

### 7. RefFusion 错误日志（MEDIUM）

- **问题核实**：注释写「只打日志」但无输出。
- **修改**：融合失败时 `log.Printf("ref fusion failed (ignored): task_id=... err=...")`。
- **文件**：`internal/service/ppt_service.go`（`runInitGeneration` 内）。

### 8. render goroutine `defer cancel()`（MEDIUM）

- **问题核实**：`cancel()` 紧接 `Render` 调用，panic 路径未取消。
- **修改**：`renderCtx, cancel := ...` 后 `defer cancel()`，再调用 `Render`。
- **文件**：`internal/service/ppt_service.go`（`runInitGeneration`）。

### 9. Export 异步（MEDIUM）

- **问题核实**：`Create` 内同步跑完导出，返回即 `completed`，GET 轮询无意义。
- **修改**：校验 `format` 后更新为 `generating`，`go runExportJob(exportID, taskID, format)`；`Create` 立即返回 `status: generating`；后台完成或失败时写回仓库。非法 format 仍同步失败。
- **文件**：`internal/service/export_service.go`。

### 10. LLM 反馈环超时（LOW）

- **问题核实**：`RunFeedbackLoop` 直接使用外层 `ctx`，可能无限阻塞占满并发槽位。
- **修改**：`ToolCallingRuntime.RunFeedbackLoop` 内 `context.WithTimeout(ctx, 8*time.Minute)` + `defer cancel()`，再调用 `r.chat`。
- **文件**：`internal/infra/llm/tool_runtime.go`。
