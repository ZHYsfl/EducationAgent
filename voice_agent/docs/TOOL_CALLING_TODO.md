# Tool Calling 重构 TODO

## 已完成 ✓
1. 创建 `tools.go` - 定义 tool schemas
2. 实现 `HandleToolCall` - tool call 处理逻辑
3. 编译通过
4. 集成 `tool_calling_go` 框架
5. 在 `NewPipeline` 中注册 PPT tools
6. 实现 `registerPPTTools` 方法
7. 实现 `toolInitPPTTask` 和 `toolModifyPPT` 工具函数
8. 添加 `detectAndExecuteToolCalls` 异步检测机制
9. 移除 `pptIntentDetectionPrompt` 字符串标记提示
10. 修改 `postProcessResponse` 使用 tool calling

## 待完成

### Phase 1: 测试验证
- [ ] 测试任务初始化流程（"帮我做一个关于高等数学的PPT"）
- [ ] 测试 PPT 修改流程（"把第一页改成..."）
- [ ] 验证 tool calling 是否正确触发
- [ ] 验证流式输出体验是否保持

### Phase 2: 清理旧代码（可选）
- [ ] 删除 `tryDetectTaskInit` 函数（保留以防回退）
- [ ] 删除 `trySendPPTFeedback` 中的标记检测（保留以防回退）
- [ ] 删除 `pptIntentDetectionPrompt` 常量（已移除）

### Phase 3: 优化
- [ ] 调整 tool calling 的 system prompt
- [ ] 优化异步检测的性能
- [ ] 添加 tool calling 的日志和监控

## 架构说明

采用 **异步 tool calling** 方案：
1. Voice Agent 保持流式输出（低延迟用户体验）
2. 流式输出完成后，异步调用 `detectAndExecuteToolCalls`
3. Tool calling 在后台执行，不阻塞对话流程
4. 工具执行结果通过现有的 WebSocket 消息反馈

这种方案符合 Step-GUI 的并行 Agent 架构思想：
- Voice Agent = 电话 Agent（实时交互）
- PPT Agent = 电脑 Agent（后台执行任务）
- 通过消息异步通信，互不阻塞

