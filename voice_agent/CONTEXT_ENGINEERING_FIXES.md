# Context Engineering Bug Fixes

## 修复的Bug

### 1. ✅ 系统提示词构建重复代码 (Critical)
**问题**: `startProcessing` 和 `buildSystemPrompt` 独立构建相同的系统提示词，导致：
- 代码重复
- `buildSystemPrompt` 调用 `drainContextQueue()` 会窃取本应给 `startProcessing` 的上下文消息

**修复**: 
- 创建 `pipeline_system_prompt.go` 统一构建逻辑
- `buildFullSystemPrompt(ctx, includeContextQueue bool)` 统一入口
- `startProcessing` 调用 `buildFullSystemPrompt(ctx, true)` - 包含上下文队列
- `buildSystemPrompt` 调用 `buildFullSystemPrompt(ctx, false)` - 不包含上下文队列（避免窃取）

**文件**: 
- 新增: `agent/pipeline_system_prompt.go`
- 修改: `agent/pipeline_process.go`

---

### 2. ✅ 上下文队列线程安全问题 (Critical)
**问题**: `drainContextQueue()` 在锁外读取 channel，多个 goroutine 同时调用会竞争

**修复**:
```go
func (p *Pipeline) drainContextQueue() []ContextMessage {
    p.pendingMu.Lock()
    defer p.pendingMu.Unlock()  // 整个函数持锁
    
    var msgs []ContextMessage
    if len(p.pendingContexts) > 0 {
        msgs = append(msgs, p.pendingContexts...)
        p.pendingContexts = nil  // 清空（不是 [:0]）
    }
    
    for {
        select {
        case msg := <-p.contextQueue:
            msgs = append(msgs, msg)
        default:
            return msgs
        }
    }
}
```

**文件**: `agent/bus.go`

---

### 3. ✅ 高优先级监听器中断后退出 (Critical)
**问题**: `highPriorityListener` 在处理 `conflict_question` 被打断后 `return`，导致监听器死亡，后续消息无法处理

**修复**: 将 `return` 改为 `continue`，监听器继续运行

**文件**: `agent/bus.go` line 66-101

---

### 4. ✅ 协议指令缺少 resolve_conflict (Medium)
**问题**: 系统提示词中的动作列表没有包含 `resolve_conflict`，LLM 不知道可以使用这个动作

**修复**: 在 `protocolInstructions` 常量中添加：
```
- resolve_conflict: @{resolve_conflict|context_id:xxx} - 回答冲突问题（多个冲突时必须指定）
```

**文件**: `agent/pipeline_system_prompt.go`

---

## 测试验证

所有测试通过：
```bash
✓ TestTryResolveConflict_SinglePending
✓ TestTryResolveConflict_MultiplePending_WithMarker
✓ TestTryResolveConflict_MultiplePending_MultipleActions
✓ TestTryResolveConflict_NoPending
✓ TestTryResolveConflict_NilClients
✓ TestBuildPendingQuestionsContext_NoQuestions
✓ TestBuildPendingQuestionsContext_SingleQuestion
✓ TestBuildPendingQuestionsContext_MultipleQuestions
```

编译成功：
```bash
go build ./...  # 无错误
```

---

## 上下文工程数据流（用于数据集制作）

### 单轮处理流程
```
用户输入
  ↓
history.AddUser(userText)
  ↓
systemPrompt = buildFullSystemPrompt(ctx, true)
  ├─ Layer 1: 基础系统提示词 (config.SystemPrompt)
  ├─ Layer 2: 需求模式覆盖 (BuildRequirementsSystemPrompt) [可选]
  ├─ Layer 3: 任务列表上下文 (buildTaskListContext)
  ├─ Layer 4: 待确认问题上下文 (buildPendingQuestionsContext)
  ├─ Layer 5: 上下文队列消息 (drainContextQueue → FormatContextForLLM)
  └─ Layer 6: 协议指令 (protocolInstructions)
  ↓
messages = history.ToOpenAIWithThoughtAndPrompt("", systemPrompt)
  → [system: systemPrompt, ...历史对话, user: userText]
  ↓
LLM 流式生成
  ↓
解析动作 + 提取可见文本 + TTS
  ↓
history.AddAssistant(finalText)
```

### 数据集格式建议
```json
{
  "messages": [
    {
      "role": "system",
      "content": "<完整构建的系统提示词，包含所有6层>"
    },
    {
      "role": "user",
      "content": "历史对话1"
    },
    {
      "role": "assistant",
      "content": "回复1（包含动作标记）"
    },
    {
      "role": "user",
      "content": "当前用户输入"
    }
  ]
}
```

### 关键点
1. **系统提示词是动态的**：每轮对话根据当前状态重新构建
2. **上下文队列只包含最近的**：不是全部历史，是最近的 RAG/搜索结果
3. **需求模式会替换基础提示词**：不是追加，是完全替换
4. **动作标记保留在训练数据中**：让模型学会生成结构化输出

---

## 修改的文件清单

1. **新增**: `agent/pipeline_system_prompt.go` - 统一系统提示词构建
2. **修改**: `agent/pipeline_process.go` - 使用统一构建器
3. **修改**: `agent/bus.go` - 修复线程安全 + 监听器退出bug
