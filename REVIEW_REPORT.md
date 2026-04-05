# Voice Agent 接口实现审查报告

**审查日期**: 2026-04-05  
**审查范围**: 所有模块对照 API_DOCUMENTATION.md 的字段匹配、逻辑正确性、时序问题

---

## 严重等级说明

- **[CRITICAL]** 功能完全不可用 / 数据丢失
- **[HIGH]** 客户端协议破坏 / 接口字段错误
- **[MEDIUM]** 状态机错误 / 时序问题 / 竞态
- **[LOW]** 轻微不一致 / 边界情况

---

## 第一部分：对外调用接口（我们调用外部服务）

外部接口字段经逐一核查均与 API 文档一致，无字段缺失或路径错误。

---

## 第二部分：我们提供的接口

### 2.1 `conflict_ask` 消息从未发送 ✅ 已修复

**[CRITICAL]** `agent/http.go` — `HandleServiceCallback`

已在 `conflict_question` 分支中添加 `conflict_ask` 消息发送。

---

### 2.2 `resolve_conflict` 动作未在 executor 中处理 ✅ 已修复

**[CRITICAL]** `internal/executor/executor.go`

已添加 `resolve_conflict` case，通过 `SendFeedback` 将用户答案转发给 PPT Agent，msgType 为 `conflict_resolved`。

---

### 2.3 `error` 消息字段名错误 ✅ 已修复

**[HIGH]** `agent/session.go`

已将 `Text` 改为 `Message`，并补充 `Code: 40001`。

---

### 2.4 `task_list_update` 从未发送，发送了非规范的 `task_created` ✅ 已修复

**[HIGH]** `agent/pipeline_ctx.go`

已将 `task_created` 改为发送 `task_list_update`，携带 `active_task_id` 和完整 `tasks` map。

---

### 2.5 `GET /api/v1/tasks/{task_id}/preview` — `pages_info` 与 `pages` 内容相同

**[MEDIUM]** `agent/http.go`

API 文档响应体同时有 `pages` 和 `pages_info` 两个字段（后者标注为"兼容字段"）。代码将同一个 slice 赋给两个字段，当前实现符合"兼容字段"的语义，**可接受**，记录备查。

---

## 第三部分：需求收集流程

### 3.1 `requirements_progress.status` 值不符合规范 ✅ 已关闭

**[HIGH]** `agent/pipeline_post.go`

API 文档已过时。`"ready"` 是当前约定的正确值，无需修改。

---

### 3.2 `requirements_summary` 是非规范消息类型 ✅ 已修复

**[HIGH]** `agent/pipeline_post.go`

`requirements_summary` 消息已保留并补入 API 文档（§11a）。所有字段收集完毕时先发 `requirements_summary`（携带 `summary_text`），再发 `requirements_progress`（`status: "ready"`）。

---

### 3.3 `collected_fields` 与 `missing_fields` 字段名不一致 ✅ 已修复

**[HIGH]** `agent/requirements.go`

已将 `RefreshCollectedFields()` 中的 `"target_audience"` 统一改为 `"audience"`，与 `GetMissingFields()` 一致。同步修复了 `executor/ppt.go` 中 `checkRequiredFields()` 的同名问题。

---

### 3.4 `subject` 字段永远不出现在 `collected_fields` ✅ 已修复

**[MEDIUM]** `agent/requirements.go`

已在 `RefreshCollectedFields()` 中添加 `subject` 字段。

---

## 第四部分：竞态与时序问题

### 4.1 需求字段在锁释放后被读取

**[MEDIUM]** `agent/pipeline_post.go` — `handleRequirementsUpdate`

代码在 `reqMu.Unlock()` 之后仍然直接读取 `req.Status`、`req.CollectedFields`、`req.GetMissingFields()`，而不是使用已加锁时拷贝的 `reqSnapshot`。并发更新时存在数据竞态。

修复：将这三处改为读 `reqSnapshot` 的对应字段。

---

### 4.2 `processContextUpdate` 使用 `context.Background()`

**[MEDIUM]** `agent/pipeline_ctx.go`

传入的是不可取消的 context。Session 关闭或新 pipeline 启动时，该 goroutine 仍会继续运行 LLM 调用，并向已关闭的 session 发送消息，造成 goroutine 泄漏。

应传入 session 生命周期绑定的 context。

---

### 4.3 `EnqueueContext` 状态检查与 goroutine 启动不原子

**[MEDIUM]** `agent/pipeline_ctx.go`

两条并发的 context 消息同时通过 idle 检查，会启动两个并发 LLM 调用，客户端收到双份响应。

---

### 4.4 interrupt 后 session 状态不归 idle

**[MEDIUM]** `agent/pipeline_process.go`

`startProcessing` 在 context 被取消时，`if ctx.Err() == nil` 的守卫跳过了 `SetState(StateIdle)`。中断后 session 永久停留在 `StateProcessing` 或 `StateSpeaking`，后续 context 消息因 idle 检查失败被静默丢弃。

---

### 4.5 `transcript` 在 LLM 已启动后才发送

**[LOW]** `agent/pipeline_process.go`

`SendJSON(transcript)` 在 `largeLLM.StreamChat` 调用之后执行。客户端可能先收到 `response` token，再收到触发它的 `transcript`，UI 顺序颠倒。

---

## 汇总表

| # | 等级 | 模块 | 问题 | 状态 |
|---|------|------|------|------|
| 1 | CRITICAL | `http.go` | `conflict_ask` 消息从未发送给客户端 | ✅ 已修复 |
| 2 | CRITICAL | `executor/executor.go` | `resolve_conflict` 动作未处理，冲突解决链路断裂 | ✅ 已修复 |
| 3 | HIGH | `session.go` | `error` 消息用 `text` 字段，应为 `message`，且缺 `code` | ✅ 已修复 |
| 4 | HIGH | `pipeline_ctx.go` | 发送非规范 `task_created`，从未发送 `task_list_update` | ✅ 已修复 |
| 5 | HIGH | `pipeline_post.go` | `requirements_progress.status` 值为 `"ready"`，API 文档已过时 | ✅ 已关闭 |
| 6 | HIGH | `pipeline_post.go` | 发送非规范 `requirements_summary` 消息类型 | ✅ 已修复 |
| 7 | HIGH | `requirements.go` | `audience` vs `target_audience` 字段名不一致，进度永远错乱 | ✅ 已修复 |
| 8 | MEDIUM | `requirements.go` | `subject` 永远不出现在 `collected_fields` | ✅ 已修复 |
| 9 | MEDIUM | `pipeline_post.go` | 锁释放后读取 `req` 字段，存在竞态 | ✅ 已修复 |
| 10 | MEDIUM | `pipeline_ctx.go` | `processContextUpdate` 使用不可取消 context，goroutine 泄漏 | 待修复 |
| 11 | MEDIUM | `pipeline_ctx.go` | idle 检查与 goroutine 启动不原子，可能双发响应 | 待修复 |
| 12 | MEDIUM | `pipeline_process.go` | interrupt 后状态不归 idle，后续消息被丢弃 | 待修复 |
| 13 | LOW | `pipeline_process.go` | `transcript` 在 LLM 启动后才发送，客户端顺序颠倒 | 待修复 |
