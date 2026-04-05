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

### 2.1 `conflict_ask` 消息从未发送

**[CRITICAL]** `agent/http.go` — `HandleServiceCallback`

API 文档定义了 `conflict_ask` 服务端→客户端消息：
```json
{ "type": "conflict_ask", "task_id": "...", "page_id": "...", "context_id": "...", "question": "..." }
```

回调处理的 switch 中，`conflict_question` 分支只设置了 `priority = "high"`，然后 fall-through 到默认分支，**没有向 WebSocket 客户端发送任何消息**。冲突询问流程完全不可用。

---

### 2.3 `resolve_conflict` 动作未在 executor 中处理

**[CRITICAL]** `internal/executor/executor.go`

系统提示（`pipeline_system_prompt.go`）指示 LLM 输出：
```
@{resolve_conflict|context_id:xxx}
```

但 `executor.Execute` 的 switch 只处理：`update_requirements`, `ppt_init`, `ppt_mod`, `kb_query`, `web_search`，**没有 `resolve_conflict` case**，落入 `default` 返回 `"Unknown action: resolve_conflict"`。

冲突解决的完整链路（PPT Agent 问 → 用户答 → Voice Agent 转发）全部断裂。

---

### 2.4 `error` 消息字段名错误

**[HIGH]** `agent/session.go`

API 文档定义 `error` 消息：
```json
{ "type": "error", "code": 0, "message": "..." }
```

代码发送：
```go
s.SendJSON(WSMessage{Type: "error", Text: "Invalid message format"})
```

字段用的是 `Text`（序列化为 `"text"`），文档要求是 `"message"`。且 `code` 字段缺失。客户端按文档解析会拿不到错误信息。

---

### 2.5 `task_list_update` 从未发送，发送了非规范的 `task_created`

**[HIGH]** `agent/pipeline_ctx.go`

API 文档定义任务列表更新消息：
```json
{ "type": "task_list_update", "active_task_id": "...", "tasks": {"task_id": "topic"} }
```

代码发送的是：
```go
WSMessage{Type: "task_created", TaskID: taskID, Topic: topic}
```

`task_created` 不在文档规范中，客户端无法处理。`task_list_update` 从未被发送，客户端任务列表 UI 永远不会更新。

---

### 2.6 `GET /api/v1/tasks/{task_id}/preview` — `pages_info` 与 `pages` 内容相同

**[MEDIUM]** `agent/http.go`

API 文档响应体同时有 `pages` 和 `pages_info` 两个字段（后者标注为"兼容字段"）。代码将同一个 slice 赋给两个字段，行为上没问题，但如果将来两者需要分离会有隐患。当前实现符合"兼容字段"的语义，**可接受**，记录备查。

---

## 第三部分：需求收集流程

### 3.1 `requirements_progress.status` 值不符合规范

**[HIGH]** `agent/pipeline_post.go`

API 文档规定 `status` 取值为：`collecting` → `confirming` → `confirmed`

代码在所有字段收集完毕后设置 `req.Status = "ready"`，发送给客户端的消息中 `status` 为 `"ready"`，客户端无法识别。

---

### 3.2 `requirements_summary` 是非规范消息类型

**[HIGH]** `agent/pipeline_post.go`

当需求收集完成时，代码发送了一个 `type: "requirements_summary"` 的消息，该消息类型不在 API 文档中。客户端收到后无法处理，需求确认卡片无法显示。

---

### 3.3 `collected_fields` 与 `missing_fields` 字段名不一致

**[HIGH]** `agent/requirements.go`

`GetMissingFields()` 报告缺失字段名为 `"audience"`，但 `RefreshCollectedFields()` 标记已收集字段名为 `"target_audience"`。

同一个字段有两个名字，导致该字段可能同时出现在 `collected_fields` 和 `missing_fields` 中，客户端进度显示永远不准确。

---

### 3.4 `subject` 字段永远不出现在 `collected_fields`

**[MEDIUM]** `agent/requirements.go`

`GetMissingFields()` 会把 `subject` 列为缺失，但 `RefreshCollectedFields()` 从不把 `subject` 加入已收集列表。即使用户提供了 subject，它也永远显示为"未收集"。

---

## 第四部分：竞态与时序问题

### 4.1 需求字段在锁释放后被读取

**[MEDIUM]** `agent/pipeline_post.go` — `handleRequirementsUpdate`

代码在 `reqMu.Unlock()` 之后仍然直接读取 `req.Status`、`req.CollectedFields`、`req.GetMissingFields()`，而不是使用已加锁时拷贝的 `reqSnapshot`。并发更新时存在数据竞态。

修复：将这三处改为读 `reqSnapshot` 的对应字段。

---

### 4.2 `processContextUpdate` 使用 `context.Background()`

**[MEDIUM]** `agent/pipeline_ctx.go`

```go
go p.processContextUpdate(context.Background(), msg)
```

传入的是不可取消的 context。Session 关闭或新 pipeline 启动时，该 goroutine 仍会继续运行 LLM 调用，并向已关闭的 session 发送消息，造成 goroutine 泄漏。

应传入 session 生命周期绑定的 context。

---

### 4.3 `EnqueueContext` 状态检查与 goroutine 启动不原子

**[MEDIUM]** `agent/pipeline_ctx.go`

```go
if p.session.GetState() == StateIdle {
    go p.processContextUpdate(...)
}
```

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

| # | 等级 | 模块 | 问题 |
|---|------|------|------|
| 3 | CRITICAL | `http.go` | `conflict_ask` 消息从未发送给客户端 |
| 4 | CRITICAL | `executor/executor.go` | `resolve_conflict` 动作未处理，冲突解决链路断裂 |
| 6 | HIGH | `session.go` | `error` 消息用 `text` 字段，应为 `message`，且缺 `code` |
| 7 | HIGH | `pipeline_ctx.go` | 发送非规范 `task_created`，从未发送 `task_list_update` |
| 8 | HIGH | `pipeline_post.go` | `requirements_progress.status` 值为 `"ready"`，应为 `"confirming"` |
| 9 | HIGH | `pipeline_post.go` | 发送非规范 `requirements_summary` 消息类型 |
| 10 | HIGH | `requirements.go` | `audience` vs `target_audience` 字段名不一致，进度永远错乱 |
| 11 | MEDIUM | `requirements.go` | `subject` 永远不出现在 `collected_fields` |
| 12 | MEDIUM | `pipeline_post.go` | 锁释放后读取 `req` 字段，存在竞态 |
| 13 | MEDIUM | `pipeline_ctx.go` | `processContextUpdate` 使用不可取消 context，goroutine 泄漏 |
| 14 | MEDIUM | `pipeline_ctx.go` | idle 检查与 goroutine 启动不原子，可能双发响应 |
| 15 | MEDIUM | `pipeline_process.go` | interrupt 后状态不归 idle，后续消息被丢弃 |
| 16 | LOW | `pipeline_process.go` | `transcript` 在 LLM 启动后才发送，客户端顺序颠倒 |






