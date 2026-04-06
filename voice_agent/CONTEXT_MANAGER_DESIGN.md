# Context Manager 设计文档

## 问题分析

当前上下文管理分散在多个地方，造数据集困难：

### 当前架构的问题

**1. 上下文来源分散**
```
session.Requirements          → 需求信息（12个字段）
session.tasks                 → 任务列表（多任务场景）
session.pendingQuestions      → 待回答的冲突问题
pipeline.pendingContexts      → 待处理消息缓存
pipeline.contextQueue         → 普通优先级消息队列
pipeline.highPriorityQueue    → 高优先级消息队列
history                       → 对话历史
```

**2. 拼接逻辑分散**
```
buildFullSystemPrompt()              → 5层动态拼接
BuildRequirementsSystemPrompt()      → 需求模式提示词
buildTaskListContext()               → 任务列表上下文
buildPendingQuestionsContext()       → 冲突问题上下文
FormatContextForLLM()                → 队列消息格式化
```

**3. 状态变更分散**
```
update_requirements  → 修改 Requirements
ppt_init            → 创建任务，修改 Requirements.Status
ppt_mod             → 可能切换 ActiveTaskID
resolve_conflict    → 移除 pendingQuestions
异步回调             → 添加消息到队列
```

### 造数据集的难点

1. **无法获取完整上下文快照**：不知道某个时刻 LLM 看到的完整输入是什么
2. **无法复现状态**：无法从某个状态点重新开始测试
3. **无法可视化**：看不到上下文的完整结构
4. **无法追踪变更**：不知道哪个操作改变了哪部分上下文

## 解决方案：统一的 ContextManager

### 核心思想

**单一职责**：所有上下文的读取、修改、拼接都通过 ContextManager

```
┌─────────────────────────────────────────┐
│         ContextManager                  │
│  ┌───────────────────────────────────┐  │
│  │  ContextSnapshot (完整状态)       │  │
│  │  ├─ Requirements                  │  │
│  │  ├─ Tasks []TaskInfo              │  │
│  │  │   ├─ task_id                   │  │
│  │  │   ├─ topic                     │  │
│  │  │   ├─ status                    │  │
│  │  │   └─ viewing_page_id           │  │
│  │  ├─ PendingQuestions []Question   │  │
│  │  │   ├─ context_id                │  │
│  │  │   ├─ task_id                   │  │
│  │  │   ├─ question                  │  │
│  │  │   └─ timestamp                 │  │
│  │  ├─ ContextMessages []Message     │  │
│  │  │   ├─ action_type               │  │
│  │  │   ├─ priority                  │  │
│  │  │   ├─ content                   │  │
│  │  │   └─ metadata                  │  │
│  │  └─ ConversationHistory []Turn    │  │
│  │      ├─ role                      │  │
│  │      └─ content                   │  │
│  └───────────────────────────────────┘  │
│                                          │
│  核心方法:                                │
│  ├─ BuildPrompt() string                │
│  ├─ Export() ContextSnapshot             │
│  ├─ Import(snapshot)                     │
│  ├─ Visualize() string                   │
│  └─ Diff(before, after) []Change         │
└─────────────────────────────────────────┘
```

### 数据结构

```go
// ContextSnapshot 完整上下文快照（可序列化为 JSON）
type ContextSnapshot struct {
    // 元信息
    Timestamp   int64  `json:"timestamp"`
    SessionID   string `json:"session_id"`
    UserID      string `json:"user_id"`
    
    // 需求信息
    Requirements *TaskRequirements `json:"requirements,omitempty"`
    
    // 任务列表
    Tasks []TaskInfo `json:"tasks"`
    ActiveTaskID string `json:"active_task_id"`
    
    // 待回答的冲突问题
    PendingQuestions []PendingQuestion `json:"pending_questions"`
    
    // 上下文消息队列（RAG/搜索结果/PPT回调）
    ContextMessages []ContextMessage `json:"context_messages"`
    
    // 对话历史
    ConversationHistory []ConversationTurn `json:"conversation_history"`
    
}

// TaskInfo 任务信息摘要
type TaskInfo struct {
    TaskID        string `json:"task_id"`
    Topic         string `json:"topic"`
    Status        string `json:"status"`
    TotalPages    int    `json:"total_pages"`
    ViewingPageID string `json:"viewing_page_id"`
    CreatedAt     int64  `json:"created_at"`
}

// PendingQuestion 待回答的冲突问题
type PendingQuestion struct {
    ContextID     string `json:"context_id"`
    TaskID        string `json:"task_id"`
    PageID        string `json:"page_id"`
    Question      string `json:"question"`
    BaseTimestamp int64  `json:"base_timestamp"`
    AskedAt       int64  `json:"asked_at"`
}

// ConversationTurn 对话轮次
type ConversationTurn struct {
    Role    string `json:"role"`    // "user" | "assistant"
    Content string `json:"content"`
}
```

### 核心方法

#### 1. BuildPrompt() - 统一拼接逻辑

```go
func (cm *ContextManager) BuildPrompt(includeContextQueue bool) string {
    var sb strings.Builder
    
    // Layer 1: Base system prompt or Requirements mode
    if cm.snapshot.Requirements != nil && 
       (cm.snapshot.Requirements.Status == "collecting" || 
        cm.snapshot.Requirements.Status == "ready") {
        sb.WriteString(cm.snapshot.Requirements.BuildRequirementsSystemPrompt())
    } else {
        sb.WriteString(cm.baseSystemPrompt)
    }
    
    // Layer 2: Task list context
    if len(cm.snapshot.Tasks) > 0 {
        sb.WriteString(cm.formatTaskListContext())
    }
    
    // Layer 3: Pending questions context
    if len(cm.snapshot.PendingQuestions) > 0 {
        sb.WriteString(cm.formatPendingQuestionsContext())
    }
    
    // Layer 4: Context queue messages
    if includeContextQueue && len(cm.snapshot.ContextMessages) > 0 {
        sb.WriteString(cm.formatContextMessages())
    }
    
    // Layer 5: Protocol instructions
    sb.WriteString(protocolInstructions)
    
    return sb.String()
}
```

#### 2. Export() - 导出快照

```go
func (cm *ContextManager) Export() ContextSnapshot {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    
    // 深拷贝当前状态
    snapshot := cm.snapshot
    snapshot.Timestamp = time.Now().UnixMilli()
    
    return snapshot
}
```

#### 3. Import() - 导入快照

```go
func (cm *ContextManager) Import(snapshot ContextSnapshot) {
    cm.mu.Lock()
    defer cm.mu.Unlock()
    
    cm.snapshot = snapshot
}
```

#### 4. Visualize() - 可视化上下文

```go
func (cm *ContextManager) Visualize() string {
    var sb strings.Builder
    
    sb.WriteString("=== Context Snapshot ===\n")
    sb.WriteString(fmt.Sprintf("Session: %s | User: %s\n", 
        cm.snapshot.SessionID, cm.snapshot.UserID))
    
    // Requirements
    if cm.snapshot.Requirements != nil {
        sb.WriteString(fmt.Sprintf("\n[Requirements] Status: %s\n", 
            cm.snapshot.Requirements.Status))
        sb.WriteString(fmt.Sprintf("  Topic: %s\n", cm.snapshot.Requirements.Topic))
        sb.WriteString(fmt.Sprintf("  Subject: %s\n", cm.snapshot.Requirements.Subject))
        // ... 其他字段
    }
    
    // Tasks
    if len(cm.snapshot.Tasks) > 0 {
        sb.WriteString(fmt.Sprintf("\n[Tasks] Count: %d | Active: %s\n", 
            len(cm.snapshot.Tasks), cm.snapshot.ActiveTaskID))
        for _, t := range cm.snapshot.Tasks {
            sb.WriteString(fmt.Sprintf("  - %s: %s (status=%s, pages=%d)\n",
                t.TaskID, t.Topic, t.Status, t.TotalPages))
        }
    }
    
    // Pending Questions
    if len(cm.snapshot.PendingQuestions) > 0 {
        sb.WriteString(fmt.Sprintf("\n[Pending Questions] Count: %d\n", 
            len(cm.snapshot.PendingQuestions)))
        for _, q := range cm.snapshot.PendingQuestions {
            sb.WriteString(fmt.Sprintf("  - [%s] %s\n", q.ContextID, q.Question))
        }
    }
    
    // Context Messages
    if len(cm.snapshot.ContextMessages) > 0 {
        sb.WriteString(fmt.Sprintf("\n[Context Messages] Count: %d\n", 
            len(cm.snapshot.ContextMessages)))
        for _, m := range cm.snapshot.ContextMessages {
            sb.WriteString(fmt.Sprintf("  - [%s|%s] %s\n", 
                m.ActionType, m.Priority, truncate(m.Content, 50)))
        }
    }
    
    // Conversation History
    sb.WriteString(fmt.Sprintf("\n[Conversation] Turns: %d\n", 
        len(cm.snapshot.ConversationHistory)))
    for i, turn := range cm.snapshot.ConversationHistory {
        sb.WriteString(fmt.Sprintf("  %d. %s: %s\n", 
            i+1, turn.Role, truncate(turn.Content, 60)))
    }
    
    return sb.String()
}
```

#### 5. Diff() - 对比两个快照

```go
func Diff(before, after ContextSnapshot) []ContextChange {
    var changes []ContextChange
    
    // 对比 Requirements
    if !reflect.DeepEqual(before.Requirements, after.Requirements) {
        changes = append(changes, ContextChange{
            Type: "requirements_changed",
            Before: before.Requirements,
            After: after.Requirements,
        })
    }
    
    // 对比 Tasks
    if len(before.Tasks) != len(after.Tasks) {
        changes = append(changes, ContextChange{
            Type: "tasks_changed",
            Desc: fmt.Sprintf("tasks count: %d → %d", 
                len(before.Tasks), len(after.Tasks)),
        })
    }
    
    // 对比 PendingQuestions
    // ...
    
    return changes
}
```

## 使用场景

### 场景1：生成微调数据集

```go
// 在每次 LLM 调用前后记录快照
func (p *Pipeline) processWithSnapshot(userText string) {
    // 调用前快照
    beforeSnapshot := p.contextMgr.Export()
    
    // 构建提示词
    systemPrompt := p.contextMgr.BuildPrompt(true)
    messages := p.history.ToOpenAIWithThoughtAndPrompt("", systemPrompt)
    
    // 调用 LLM
    response := p.largeLLM.StreamChat(ctx, messages)
    
    // 调用后快照
    afterSnapshot := p.contextMgr.Export()
    
    // 保存训练样本
    sample := TrainingSample{
        Input: TrainingInput{
            SystemPrompt: systemPrompt,
            UserMessage: userText,
            ContextSnapshot: beforeSnapshot,
        },
        Output: response,
        ContextChanges: Diff(beforeSnapshot, afterSnapshot),
    }
    saveTrainingSample(sample)
}
```

### 场景2：可视化调试

```go
// 在关键节点打印上下文
func (p *Pipeline) debugContext() {
    log.Println(p.contextMgr.Visualize())
}

// 输出示例：
// === Context Snapshot ===
// Session: sess_123 | User: user_456
//
// [Requirements] Status: collecting
//   Topic: 高等数学
//   Subject: 数学
//   Audience: 大学生
//   TotalPages: 0 (未设置)
//
// [Tasks] Count: 0 | Active: 
//
// [Pending Questions] Count: 0
//
// [Context Messages] Count: 1
//   - [kb_query|normal] 检索到3条相关知识点：1. 极限的定义...
//
// [Conversation] Turns: 4
//   1. user: 帮我做个高等数学的PPT
//   2. assistant: 好的。@{update_requirements|topic:高等数学} 请问...
//   3. user: 大学生
//   4. assistant: 明白了。@{update_requirements|audience:大学生} ...
```

### 场景3：回归测试

```go
// 从快照恢复状态，测试特定场景
func TestMultiTaskScenario(t *testing.T) {
    // 加载预设快照（2个任务，1个冲突问题）
    snapshot := loadSnapshot("testdata/multi_task_with_conflict.json")
    
    cm := NewContextManager()
    cm.Import(snapshot)
    
    // 模拟用户输入
    userInput := "把物理课件的第3页改成蓝色"
    
    // 验证上下文拼接
    prompt := cm.BuildPrompt(true)
    assert.Contains(t, prompt, "当前有2个任务")
    assert.Contains(t, prompt, "待回答的冲突问题")
    
    // 验证 LLM 输出
    response := callLLM(prompt, userInput)
    assert.Contains(t, response, "@{ppt_mod|task:task_physics")
}
```

### 场景4：导出数据集

```go
// 批量导出所有会话的快照
func ExportDataset(sessions []*Session) {
    var dataset []DatasetEntry
    
    for _, sess := range sessions {
        snapshots := sess.GetAllSnapshots()
        for i, snap := range snapshots {
            if i == 0 {
                continue // 跳过初始状态
            }
            
            prevSnap := snapshots[i-1]
            
            entry := DatasetEntry{
                ID: fmt.Sprintf("%s_%d", sess.SessionID, i),
                Context: ContextToPrompt(prevSnap),
                Input: snap.ConversationHistory[len(prevSnap.ConversationHistory)].Content,
                Output: snap.ConversationHistory[len(snap.ConversationHistory)-1].Content,
                Metadata: map[string]any{
                    "session_id": sess.SessionID,
                    "turn": i,
                    "has_tasks": len(snap.Tasks) > 0,
                    "has_conflicts": len(snap.PendingQuestions) > 0,
                },
            }
            dataset = append(dataset, entry)
        }
    }
    
    saveJSON("dataset.jsonl", dataset)
}
```

## 实现计划

### Phase 1: 基础结构（1-2小时）
- [ ] 创建 `agent/context_manager.go`
- [ ] 定义 `ContextSnapshot` 结构体
- [ ] 实现 `Export()` / `Import()` 方法
- [ ] 实现 `Visualize()` 方法

### Phase 2: 集成到 Pipeline（1-2小时）
- [ ] 在 `Pipeline` 中添加 `contextMgr *ContextManager`
- [ ] 重构 `buildFullSystemPrompt()` 使用 `contextMgr.BuildPrompt()`
- [ ] 在关键操作后更新快照（update_requirements, ppt_init, etc.）

### Phase 3: 数据集生成工具（2-3小时）
- [ ] 创建 `tools/export_dataset.go`
- [ ] 实现快照持久化（每次 LLM 调用前后保存）
- [ ] 实现数据集导出脚本
- [ ] 生成示例数据集

### Phase 4: 测试和文档（1小时）
- [ ] 编写单元测试
- [ ] 编写使用文档
- [ ] 提供示例快照文件

## 数据集格式

### 微调数据集格式（OpenAI fine-tuning）

```jsonl
{"messages": [
  {"role": "system", "content": "[完整系统提示词，包含5层上下文]"},
  {"role": "user", "content": "帮我做个高等数学的PPT"},
  {"role": "assistant", "content": "好的。@{update_requirements|topic:高等数学} 请问目标听众是谁？"}
]}
{"messages": [
  {"role": "system", "content": "[更新后的系统提示词，包含 topic=高等数学]"},
  {"role": "user", "content": "大学生"},
  {"role": "assistant", "content": "明白了。@{update_requirements|audience:大学生} 需要多少页？"}
]}
```

### 评估数据集格式

```json
{
  "id": "eval_001",
  "scenario": "multi_task_with_conflict",
  "context_snapshot": {
    "tasks": [
      {"task_id": "task_math", "topic": "高等数学"},
      {"task_id": "task_physics", "topic": "大学物理"}
    ],
    "pending_questions": [
      {"context_id": "ctx_123", "question": "页面3的标题冲突..."}
    ]
  },
  "input": "把物理课件的第3页改成蓝色",
  "expected_output": "@{ppt_mod|task:task_physics|raw_text:把第3页改成蓝色}",
  "evaluation_criteria": {
    "must_contain": ["@{ppt_mod", "task:task_physics"],
    "must_not_contain": ["task:task_math"]
  }
}
```

## 总结

通过 ContextManager：
1. ✅ **统一管理**：所有上下文逻辑集中在一个地方
2. ✅ **可观测**：随时可以看到完整上下文结构
3. ✅ **可复现**：可以从任意快照恢复状态
4. ✅ **可追踪**：可以对比快照看到变更
5. ✅ **易于造数据**：导出快照即可生成训练样本

这样就能清晰地看到上下文管理过程，方便生成高质量的微调数据集。
