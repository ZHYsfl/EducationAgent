# PPT Agent API 接口审计报告

**审计日期**: 2026-04-06  
**审计范围**: `zcxppt/internal/http/router.go` 中定义的所有接口

## 1. 接口使用情况总览

### 1.1 正在使用的接口 ✅

| 接口路径 | 方法 | 调用方 | 用途 |
|---------|------|--------|------|
| `/api/v1/ppt/init` | POST | Voice Agent | 初始化 PPT 任务 |
| `/api/v1/ppt/feedback` | POST | Voice Agent | 发送修改反馈 |
| `/api/v1/canvas/status` | GET | Voice Agent | 获取画布状态 |
| `/api/v1/canvas/vad-event` | POST | Voice Agent | 通知 VAD 事件 |

**调用位置**: `voice_agent/internal/clients/service_clients.go`

### 1.2 未使用的接口

#### A. 应该使用但未使用 ⚠️

| 接口路径 | 方法 | Handler | 功能 | 问题分析 | 建议 |
|---------|------|---------|------|---------|------|
| `/api/v1/ppt/generate_pages` | POST | `FeedbackHandler.GeneratePages` | 批量并发生成多个页面 | `/ppt/init` 可能是串行生成页面，效率低 | `/ppt/init` 内部应该调用 `GeneratePages` 实现并发生成 |
| `/api/v1/internal/feedback/timeout_tick` | POST | `FeedbackHandler.TickTimeout` | 处理超时的冲突解决 | 当用户未回答冲突问题时，需要定时清理 | 添加 cron job 定期调用（建议每分钟） |

#### B. 功能缺失，应该补充 ✅

| 接口路径 | 方法 | Handler | 功能 | 问题分析 | 建议 |
|---------|------|---------|------|---------|------|
| `/api/v1/ppt/export` | POST | `ExportHandler.Create` | 创建导出任务（pptx/pdf） | 用户生成完 PPT 后需要下载 | 添加到 Voice Agent 的工具列表 |
| `/api/v1/ppt/export/:export_id` | GET | `ExportHandler.Get` | 查询导出任务状态 | 配合 export 使用，轮询导出进度 | 添加到 Voice Agent 的工具列表 |

#### C. 可能冗余，需要进一步确认 🤔

| 接口路径 | 方法 | Handler | 功能 | 问题分析 | 建议 |
|---------|------|---------|------|---------|------|
| `/api/v1/ppt/page/:page_id/render` | GET | `PPTHandler.PageRender` | 获取单个页面的渲染结果 | `/canvas/status` 已经返回所有页面的 `render_url` | 如果 `render_url` 是完整 URL，此接口冗余；否则保留 |

#### D. 确认为死代码，应该删除 ❌

| 接口路径 | 方法 | Handler | 功能 | 问题分析 | 建议 |
|---------|------|---------|------|---------|------|
| `/api/v1/tasks` | POST | `TaskHandler.Create` | 创建任务 | `/ppt/init` 已经创建任务 | **删除** |
| `/api/v1/tasks/:task_id` | GET | `TaskHandler.Get` | 获取任务详情 | `/canvas/status` 已经获取任务状态 | **删除** |
| `/api/v1/tasks/:task_id/status` | PUT | `TaskHandler.UpdateStatus` | 更新任务状态 | 任务状态由内部逻辑管理，不应暴露 | **删除** |
| `/api/v1/tasks` | GET | `TaskHandler.List` | 列出任务列表 | 无调用方，功能重复 | **删除** |
| `/api/v1/tasks/:task_id/preview` | GET | `PPTHandler.CanvasStatus` | 预览任务（PPT Agent 版本） | Voice Agent 已实现自己的 preview 接口 | **删除** |

**注意**: Voice Agent 有自己的 `GET /api/v1/tasks/{task_id}/preview` 接口（在 `voice_agent/main.go` 中实现），它内部调用 PPT Agent 的 `/canvas/status`，然后做数据转换。PPT Agent 的 `/tasks/:task_id/preview` 路由从未被调用。

## 2. 详细分析

### 2.1 `/api/v1/ppt/generate_pages` 未被使用的原因

**代码位置**: `zcxppt/internal/http/handlers/feedback_handler.go:82-119`

**功能**:
```go
func (h *FeedbackHandler) GeneratePages(c *gin.Context) {
    // 批量并发生成多个页面
    // 支持从 RawText 解析 Intents
    // 调用 feedbackService.GeneratePages() 并发执行
}
```

**问题**:
- `/ppt/init` 初始化时需要生成 `total_pages` 个页面
- 如果串行生成，20 页可能需要几分钟
- `GeneratePages` 提供了并发生成能力，但未被 `Init` 调用

**建议**:
```go
// pptService.Init() 内部应该：
func (s *PPTService) Init(req model.PPTInitRequest) (string, string, error) {
    // 1. 创建任务
    taskID := createTask(req)
    
    // 2. 调用 GeneratePages 并发生成所有页面
    batchReq := model.BatchGeneratePagesRequest{
        TaskID:        taskID,
        BaseTimestamp: time.Now().UnixMilli(),
        RawText:       req.Description,
        Intents:       buildInitialIntents(req.TotalPages),
    }
    s.feedbackService.GeneratePages(ctx, batchReq)
    
    return taskID, "processing", nil
}
```

### 2.2 `/api/v1/internal/feedback/timeout_tick` 未被使用的原因

**代码位置**: `zcxppt/internal/http/handlers/feedback_handler.go:73-79`

**功能**:
```go
func (h *FeedbackHandler) TickTimeout(c *gin.Context) {
    // 处理超时的冲突解决
    // 扫描所有 suspended_for_human 状态且超时的页面
    // 自动降级处理或标记为失败
}
```

**问题**:
- 当 LLM 生成的页面有冲突时，会进入 `suspended_for_human` 状态
- 如果用户长时间不回答，这些页面会一直挂起
- 需要定时任务清理超时的挂起状态

**建议**:
1. 使用系统 cron 或 Kubernetes CronJob 定期调用
2. 或者在 PPT Agent 内部启动 goroutine 定时调用
3. 建议频率：每 1 分钟检查一次

**实现示例**（内部定时器）:
```go
// cmd/server/main.go
func startTimeoutTicker(feedbackService *service.FeedbackService) {
    ticker := time.NewTicker(1 * time.Minute)
    go func() {
        for range ticker.C {
            ctx := context.Background()
            if err := feedbackService.ProcessTimeoutTick(ctx); err != nil {
                log.Printf("timeout tick failed: %v", err)
            }
        }
    }()
}
```

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

### 3.1 立即执行（本次清理）

- [x] 删除 `POST /api/v1/tasks`
- [x] 删除 `GET /api/v1/tasks/:task_id`
- [x] 删除 `PUT /api/v1/tasks/:task_id/status`
- [x] 删除 `GET /api/v1/tasks`
- [x] 删除 `GET /api/v1/tasks/:task_id/preview`
- [x] 删除 `TaskHandler` 及相关代码

### 3.2 后续优化（待实现）

#### 优先级 P0（核心功能缺失）
- [ ] 添加导出功能到 Voice Agent 工具列表
- [ ] 实现 timeout_tick 定时任务

#### 优先级 P1（性能优化）
- [ ] `/ppt/init` 内部调用 `GeneratePages` 实现并发生成
- [ ] 性能测试：对比串行 vs 并发生成时间

#### 优先级 P2（代码清理）
- [ ] 确认 `PageRender` 是否冗余，如果是则删除
- [ ] 更新 API 文档，移除已删除的接口

## 4. 风险评估

### 4.1 删除 `/tasks/*` 的风险

**风险等级**: 🟢 低

**理由**:
1. 代码搜索确认无任何调用方
2. Voice Agent 不依赖这些接口
3. 前端（如果有）也不直接调用 PPT Agent

**回滚方案**:
- Git 历史中保留完整代码
- 如果发现遗漏的调用方，可以快速恢复

### 4.2 添加导出功能的风险

**风险等级**: 🟡 中

**理由**:
1. 需要实现文件生成逻辑（python-pptx）
2. 需要处理大文件上传到 OSS
3. 需要异步任务队列

**建议**:
- 先实现 pptx 导出（python-pptx 库成熟）
- pdf 导出可以后续添加（需要 LibreOffice 或其他转换工具）

## 5. 附录

### 5.1 相关文件清单

**需要修改的文件**:
- `zcxppt/internal/http/router.go` - 删除路由定义
- `zcxppt/internal/http/handlers/task_handler.go` - 删除整个文件
- `zcxppt/internal/service/task_service.go` - 检查是否还需要
- `zcxppt/cmd/server/main.go` - 删除 TaskHandler 初始化

**需要保留的文件**:
- `zcxppt/internal/repository/task_repository.go` - 任务存储逻辑仍需要
- `zcxppt/internal/model/*` - 数据模型仍需要

### 5.2 测试清单

删除代码后需要验证：
- [ ] `go build ./...` 编译通过
- [ ] `go test ./...` 测试通过
- [ ] Voice Agent 集成测试通过
- [ ] `/ppt/init` 功能正常
- [ ] `/canvas/status` 功能正常

---

**审计人**: Claude  
**审核状态**: 待执行  
**最后更新**: 2026-04-06
