# zcxppt 接口实现审查报告

**审查日期**: 2026-04-05
**审查范围**: `zcxppt` 所有模块，对照 `voice_agent` 实际调用代码双重验证

---

## 严重等级

- **[CRITICAL]** 功能完全不可用
- **[HIGH]** 核心链路破坏
- **[MEDIUM]** 状态错误 / 竞态
- **[LOW]** 代码质量 / 边界情况

---

## POST /api/v1/ppt/init

### 1.1 Init 同步阻塞，违反异步契约

**[CRITICAL]** `internal/service/ppt_service.go:78-183`

`Init()` 同步执行 KB 查询 → RefFusion → LLM 生成 → 并行渲染（每页最长 2 分钟），voice_agent 调用后长时间阻塞，HTTP 超时后生成流程中断。

修复：创建 task 后立即返回 `task_id`，生成流程放入后台 goroutine。

---

### 1.2 `teaching_elements` 未传入 LLM

**[HIGH]** `internal/service/ppt_service.go:337-345`

voice_agent 发送了完整的 `teaching_elements`，但 `generateInitialPages` 构建 prompt 时完全未引用，核心教学元数据静默丢弃。

---

### 1.3 RefFusion 错误静默，无日志

**[MEDIUM]** `internal/service/ppt_service.go:106-109`

注释写"只打日志"，但无任何 `log.Printf`，错误完全静默。

---

### 1.4 render goroutine context cancel 未 defer

**[MEDIUM]** `internal/service/ppt_service.go:144`

`cancel()` 未 defer，renderer panic 时 context 泄漏。改为 `defer cancel()`。

---

## POST /api/v1/ppt/feedback

### 2.1 `global_modify` 与 `reorder` case 结构错误

**[HIGH]** `internal/service/feedback_service.go:216-247`

`reorder` 处理逻辑嵌入在 `global_modify` case 内，导致：
- 每次 `global_modify` 都额外执行一次 `handleReorder`
- `case "reorder":` 永远不可达，reorder 功能完全失效

---

### 2.2 `ProcessTimeoutTick` 无并发保护

**[MEDIUM]** `internal/service/feedback_service.go:485`

两个并发 tick 可同时读取同一批过期 suspend，导致双重重试/双重解决。

---

### 2.3 LLM 调用无超时

**[LOW]** `internal/infra/llm/tool_runtime.go:98`

`RunFeedbackLoop` 中 LLM 无独立 deadline，挂起时 goroutine 无限期持有 semaphore slot。

---

## GET /api/v1/canvas/status 与 VAD 事件

### 3.1 VAD 事件不持久化 `current_viewing_page_id`

**[HIGH]** `internal/service/ppt_service.go:485-507`

voice_agent 发送 VAD 事件携带 `viewing_page_id`，但 `HandleVADEvent` 从未将其写回 canvas 状态，`PPTRepository` 接口也无对应更新方法。后续 canvas status 返回的 `current_viewing_page_id` 永远是初始值。

---

### 3.2 初始页面状态为 `"completed"` 而非 `"rendering"`

**[MEDIUM]** `internal/repository/ppt_repository.go:56-58`

`InitCanvas` 将所有页面初始状态设为 `"completed"`，渲染尚未开始，voice_agent 读取 canvas status 会误认为页面已就绪。

---

## Export 接口

### 4.1 Export 同步阻塞，轮询无意义

**[MEDIUM]** `internal/service/export_service.go:33-78`

`Create` 同步执行完整导出，返回时 status 已是 `completed` 或 `failed`，GET 轮询接口是死代码。

---

## 汇总表

| # | 等级 | 模块 | 问题 | 状态 |
|---|------|------|------|------|
| 1 | CRITICAL | `ppt_service.go` | Init 同步阻塞 | 待修复 |
| 2 | HIGH | `ppt_service.go` | `teaching_elements` 未传入 LLM | 待修复 |
| 3 | HIGH | `feedback_service.go` | `global_modify`/`reorder` 结构错误 | 待修复 |
| 4 | HIGH | `ppt_service.go` | VAD 不持久化 `current_viewing_page_id` | 待修复 |
| 5 | MEDIUM | `ppt_repository.go` | 初始页面状态为 `"completed"` | 待修复 |
| 6 | MEDIUM | `feedback_service.go` | `ProcessTimeoutTick` 无并发保护 | 待修复 |
| 7 | MEDIUM | `ppt_service.go` | RefFusion 错误静默无日志 | 待修复 |
| 8 | MEDIUM | `ppt_service.go` | render context cancel 未 defer | 待修复 |
| 9 | MEDIUM | `export_service.go` | Export 同步阻塞 | 待修复 |
| 10 | LOW | `tool_runtime.go` | LLM 调用无超时 | 待修复 |
