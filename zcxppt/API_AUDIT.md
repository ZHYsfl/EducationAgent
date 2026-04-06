# PPT Agent API 接口审计报告

**审计日期**: 2026-04-06  
**审计范围**: `zcxppt/internal/http/router.go` 中定义的所有接口

## 1. 接口使用情况总览

### 1.1 外部 API（4个，由 Voice Agent 调用）

| 接口路径 | 方法 | 调用方 | 用途 |
|---------|------|--------|------|
| `/api/v1/ppt/init` | POST | Voice Agent | 初始化 PPT 任务 |
| `/api/v1/ppt/feedback` | POST | Voice Agent | 发送修改反馈 |
| `/api/v1/canvas/status` | GET | Voice Agent | 获取画布状态 |
| `/api/v1/canvas/vad-event` | POST | Voice Agent | 通知 VAD 事件 |

### 1.2 内部 API（PPT Agent 内部使用，外部不暴露）

| 接口路径 | 方法 | 功能 | 调用方 |
|---------|------|------|--------|
| `/internal/feedback/generate_pages` | POST | 批量并发生成多个页面 | PPT Agent 内部 Init 时调用 |
| `/internal/feedback/timeout_tick` | POST | 处理超时的冲突问答（已废弃） | **已废弃**，改用内部 goroutine |
| `/internal/ppt/export` | POST | 创建导出任务 | Voice Agent 调用工具时 |
| `/internal/ppt/export/:export_id` | GET | 查询导出任务状态 | Voice Agent 轮询时 |
| `/internal/ppt/page/:page_id/render` | GET | 获取单个页面渲染结果 | 内部/调试用 |

## 2. 详细分析

### 2.1 `/internal/feedback/generate_pages` 内部并发生成

**代码位置**: `zcxppt/internal/http/handlers/feedback_handler.go:82-119`

**功能**:
```go
func (h *FeedbackHandler) GeneratePages(c *gin.Context) {
    // 批量并发生成多个页面
    // 支持从 RawText 解析 Intents
    // 调用 feedbackService.GeneratePages() 并发执行
}
```

**使用场景**: `POST /api/v1/ppt/init` 初始化时，在 PPT Agent 内部调用 `GeneratePages` 并发生成所有页面。

### 2.2 冲突问答超时处理：内部 Goroutine

**代码位置**: `zcxppt/cmd/server/main.go`

**方案**: PPT Agent 启动时在后台启动一个 goroutine，每 45 秒检查一次超时的冲突问答。

```go
func startTimeoutTicker(feedbackService *service.FeedbackService) {
    go func() {
        ticker := time.NewTicker(45 * time.Second)
        defer ticker.Stop()
        for range ticker.C {
            ctx := context.Background()
            if err := feedbackService.ProcessTimeoutTick(ctx); err != nil {
                log.Printf("[timeout_ticker] tick failed: %v", err)
            }
        }
    }()
}
```

**ProcessTimeoutTick 逻辑**:
1. 查询所有已到期的挂起项（`ListExpiredSuspends`）
2. 重试次数 < 3：增加重试次数，重新设置 45 秒后过期，发送 `conflict_question` 消息（**高优先级**）
3. 重试次数已达 3 次：标记解决，取队列中下一个 pending 项继续处理

**不需要外部 cron 调用** `/internal/feedback/timeout_tick`，该路由保留仅用于调试/手动触发。

### 2.3 导出功能缺失

**代码位置**: `zcxppt/internal/http/handlers/export_handler.go`

**功能**:
- `POST /api/v1/ppt/export`: 创建导出任务（pptx/pdf）
- `GET /api/v1/ppt/export/:export_id`: 查询导出进度

**问题**:
- 用户生成完 PPT 后无法下载
- Voice Agent 没有提供"导出课件"工具

**建议**:
在 Voice Agent 中添加工具：
```python
{
    "name": "export_ppt",
    "description": "导出 PPT 课件为 pptx 或 pdf 文件",
    "parameters": {
        "task_id": "任务ID",
        "format": "pptx 或 pdf"
    }
}
```

调用流程：
1. 用户："帮我导出这个课件"
2. Voice Agent 调用 `POST /api/v1/ppt/export`
3. 返回 `export_id`
4. Voice Agent 轮询 `GET /api/v1/ppt/export/:export_id`
5. 导出完成后返回下载链接

### 2.4 `/tasks/*` 系列接口为何是死代码

**原因分析**:

1. **任务创建**: `/ppt/init` 已经创建任务，不需要单独的 `POST /tasks`
2. **任务查询**: `/canvas/status` 已经返回任务状态，不需要 `GET /tasks/:task_id`
3. **状态更新**: 任务状态由内部逻辑管理（页面生成完成自动更新），不应暴露 `PUT /tasks/:task_id/status`
4. **任务列表**: 无调用方需要列出所有任务
5. **任务预览**: Voice Agent 实现了自己的 preview 接口

**架构设计**:
- PPT Agent 是**无状态服务**，任务状态存储在 Redis
- Voice Agent 是**有状态服务**，管理会话和任务关联
- 任务管理应该由 Voice Agent 负责，PPT Agent 只提供核心功能

## 3. 行动计划

### 3.1 已完成

- [x] 删除 `POST /api/v1/tasks` 等死代码接口
- [x] 删除 `TaskHandler` 及相关代码
- [x] 将内部接口统一迁移到 `/internal/` 前缀
- [x] 实现 `startTimeoutTicker` goroutine，替代外部 cron

### 3.2 后续优化（待实现）

#### 优先级 P0（核心功能缺失）
- [ ] 添加导出功能到 Voice Agent 工具列表
- [ ] `/ppt/init` 内部调用 `GeneratePages` 实现并发生成

#### 优先级 P1（代码清理）
- [ ] 确认 `PageRender` 是否冗余，如果是则删除
- [ ] 更新 API 文档，移除已删除的接口

## 4. 风险评估

### 4.1 路由变更的风险

**风险等级**: 🟡 中

**理由**:
1. 外部 4 个 API 路径不变，Voice Agent 无需修改
2. `/internal/` 下的接口路径有变化（如 `/internal/feedback/generate_pages`），需要确保 PPT Agent 内部调用方同步更新

**回滚方案**:
- Git 历史中保留完整代码

### 4.2 内部 Goroutine 风险

**风险等级**: 🟢 低

**理由**:
1. goroutine 独立运行，不影响 HTTP 请求处理
2. `ProcessTimeoutTick` 内部有 `timeoutTickMu` 互斥锁保证并发安全

## 5. 附录

### 5.1 路由结构

```
外部 API（Voice Agent 调用）:
  POST /api/v1/ppt/init
  POST /api/v1/ppt/feedback
  GET  /api/v1/canvas/status
  POST /api/v1/canvas/vad-event

内部 API（PPT Agent 内部使用）:
  POST /internal/feedback/generate_pages   # Init 时内部调用
  POST /internal/feedback/timeout_tick      # 手动触发/调试（正常由 goroutine 调用）
  POST /internal/ppt/export
  GET  /internal/ppt/export/:export_id
  GET  /internal/ppt/page/:page_id/render
```

### 5.2 相关文件清单

**需要修改的文件**:
- `zcxppt/internal/http/router.go` - 重组路由分组
- `zcxppt/cmd/server/main.go` - 添加 `startTimeoutTicker` 启动函数
- `zcxppt/API_AUDIT.md` - 更新文档

### 5.2 测试清单

删除代码后需要验证：
- [ ] `go build ./...` 编译通过
- [ ] `go test ./...` 测试通过
- [ ] Voice Agent 集成测试通过
- [ ] `/ppt/init` 功能正常
- [ ] `/canvas/status` 功能正常

---

**审计人**: Claude
**审核状态**: 已执行
**最后更新**: 2026-04-06
