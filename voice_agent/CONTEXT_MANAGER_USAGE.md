# Context Manager 使用指南

## 快速开始

Context Manager 已经实现并通过测试，可以直接使用。

### 1. 基本使用

```go
// 在 Session 中添加 ContextManager
session := &Session{
    SessionID: "sess_123",
    UserID:    "user_456",
    // ... 其他字段
}

// 创建 ContextManager
cm := NewContextManager(session)

// 导出快照
snapshot := cm.Export()

// 可视化上下文（调试用）
fmt.Println(cm.Visualize())

// 导出 JSON（保存数据集用）
jsonStr, err := cm.ExportJSON()
if err != nil {
    log.Printf("export failed: %v", err)
}
```

### 2. 在 Pipeline 中记录快照

在 `pipeline_process.go` 的关键位置添加快照记录：

```go
func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
    // ===== 调用 LLM 前：记录快照 =====
    beforeSnapshot := p.session.contextMgr.Export()
    
    // 现有的 LLM 调用逻辑
    systemPrompt := p.buildFullSystemPrompt(ctx, true)
    messages := p.history.ToOpenAIWithThoughtAndPrompt("", systemPrompt)
    tokenCh := p.largeLLM.StreamChat(ctx, messages)
    
    // ... 处理 tokens ...
    
    // ===== 调用 LLM 后：记录快照 =====
    afterSnapshot := p.session.contextMgr.Export()
    
    // 保存训练样本
    p.saveTrainingSample(beforeSnapshot, afterSnapshot, userText, finalResponse)
}
```

### 3. 生成微调数据集

```go
// 保存训练样本
func (p *Pipeline) saveTrainingSample(
    before ContextSnapshot,
    after ContextSnapshot,
    userInput string,
    assistantOutput string,
) {
    sample := TrainingSample{
        ID:        fmt.Sprintf("%s_%d", before.SessionID, before.Timestamp),
        Timestamp: time.Now().UnixMilli(),
        
        // 输入
        SystemPrompt: p.buildFullSystemPrompt(context.Background(), true),
        UserMessage:  userInput,
        
        // 上下文快照
        ContextBefore: before,
        ContextAfter:  after,
        
        // 输出
        AssistantMessage: assistantOutput,
        
        // 变更
        Changes: Diff(before, after),
        
        // 元数据
        Metadata: map[string]any{
            "has_requirements": before.Requirements != nil,
            "task_count":       len(before.Tasks),
            "conflict_count":   len(before.PendingQuestions),
            "message_count":    len(before.ContextMessages),
            "turn_count":       len(before.ConversationHistory),
        },
    }
    
    // 追加到 JSONL 文件
    appendToJSONL("dataset/training_samples.jsonl", sample)
}

type TrainingSample struct {
    ID               string            `json:"id"`
    Timestamp        int64             `json:"timestamp"`
    SystemPrompt     string            `json:"system_prompt"`
    UserMessage      string            `json:"user_message"`
    AssistantMessage string            `json:"assistant_message"`
    ContextBefore    ContextSnapshot   `json:"context_before"`
    ContextAfter     ContextSnapshot   `json:"context_after"`
    Changes          []ContextChange   `json:"changes"`
    Metadata         map[string]any    `json:"metadata"`
}
```

### 4. 可视化调试

在关键位置打印上下文：

```go
// 在 update_requirements 后
func (p *Pipeline) handleUpdateRequirements(...) {
    // ... 更新逻辑 ...
    
    // 调试：打印当前上下文
    if p.config.Debug {
        log.Println(p.session.contextMgr.Visualize())
    }
}
```

输出示例：

```
╔════════════════════════════════════════════════════════════════╗
║              Context Snapshot Visualization                   ║
╚════════════════════════════════════════════════════════════════╝

📅 Timestamp: 2026-04-06 15:30:45
👤 Session: sess_abc123 | User: user_xyz789

┌─ 📋 Requirements ─────────────────────────────────────────────┐
│ Status: collecting
│ Topic: 高等数学
│ Subject: 数学
│ Audience: 大学生
│ Total Pages: 0
│ Knowledge Points: 3 items
└───────────────────────────────────────────────────────────────┘

┌─ 📚 Tasks (Count: 2, Active: task_math_123) ─────────────────┐
│ ▶ 1. [task_math_123] 高等数学
│    Status: unknown | Pages: 0 | Viewing: page_3
│   2. [task_phys_456] 大学物理
│    Status: unknown | Pages: 0 | Viewing: page_3
└───────────────────────────────────────────────────────────────┘

┌─ ❓ Pending Questions (Count: 1) ───────────────────────────┐
│ 1. [ctx_789] Task: task_math_123 | Page: page_3
│    Q: 页面3的标题冲突，请选择：A) 保留原标题 B) 使用新标题
└───────────────────────────────────────────────────────────────┘

┌─ 💬 Context Messages (Count: 1) ────────────────────────────┐
│ 1. ○ [kb_query | normal]
│    检索到3条相关知识点：1. 极限的定义 2. 连续性...
└───────────────────────────────────────────────────────────────┘

┌─ 💭 Conversation History (Turns: 4) ────────────────────────┐
│ 1. 👤 user: 帮我做个高等数学的PPT
│ 2. 🤖 assistant: 好的。@{update_requirements|topic:高等数学...
│ 3. 👤 user: 大学生
│ 4. 🤖 assistant: 明白了。@{update_requirements|audience:大...
└───────────────────────────────────────────────────────────────┘
```

### 5. 对比快照变更

```go
// 在操作前后对比
before := cm.Export()

// 执行操作
p.handlePPTInit(...)

after := cm.Export()

// 查看变更
changes := Diff(before, after)
for _, change := range changes {
    log.Printf("[Context Change] %s: %s", change.Type, change.Desc)
}

// 输出示例：
// [Context Change] requirements_status_changed: Status: collecting → ready
// [Context Change] tasks_count_changed: Tasks count: 0 → 1
```

## 数据集格式

### OpenAI Fine-tuning 格式

从 `TrainingSample` 转换为 OpenAI 格式：

```go
func ConvertToOpenAIFormat(sample TrainingSample) map[string]any {
    return map[string]any{
        "messages": []map[string]string{
            {"role": "system", "content": sample.SystemPrompt},
            {"role": "user", "content": sample.UserMessage},
            {"role": "assistant", "content": sample.AssistantMessage},
        },
    }
}
```

生成 JSONL：

```bash
# 从 training_samples.jsonl 转换为 openai_format.jsonl
go run tools/convert_dataset.go \
    --input dataset/training_samples.jsonl \
    --output dataset/openai_format.jsonl \
    --format openai
```

### 评估数据集格式

```json
{
  "id": "eval_001",
  "scenario": "multi_task_with_conflict",
  "context_snapshot": {
    "session_id": "sess_123",
    "tasks": [
      {"task_id": "task_math", "topic": "高等数学"},
      {"task_id": "task_physics", "topic": "大学物理"}
    ],
    "pending_questions": [
      {
        "context_id": "ctx_123",
        "task_id": "task_math",
        "question": "页面3的标题冲突..."
      }
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

## 工具脚本

### 导出所有会话的快照

```go
// tools/export_snapshots.go
package main

import (
    "encoding/json"
    "log"
    "os"
    "voiceagent/agent"
)

func main() {
    // 假设有一个全局的 session registry
    sessions := agent.GetAllSessions()
    
    for _, sess := range sessions {
        cm := agent.NewContextManager(sess)
        snapshot := cm.Export()
        
        filename := fmt.Sprintf("snapshots/%s_%d.json", 
            sess.SessionID, snapshot.Timestamp)
        
        data, _ := json.MarshalIndent(snapshot, "", "  ")
        os.WriteFile(filename, data, 0644)
        
        log.Printf("Exported snapshot: %s", filename)
    }
}
```

### 批量生成训练数据

```go
// tools/generate_dataset.go
package main

import (
    "bufio"
    "encoding/json"
    "log"
    "os"
)

func main() {
    input, _ := os.Open("dataset/training_samples.jsonl")
    defer input.Close()
    
    output, _ := os.Create("dataset/openai_format.jsonl")
    defer output.Close()
    
    scanner := bufio.NewScanner(input)
    writer := bufio.NewWriter(output)
    
    count := 0
    for scanner.Scan() {
        var sample TrainingSample
        json.Unmarshal(scanner.Bytes(), &sample)
        
        // 转换为 OpenAI 格式
        openaiFormat := map[string]any{
            "messages": []map[string]string{
                {"role": "system", "content": sample.SystemPrompt},
                {"role": "user", "content": sample.UserMessage},
                {"role": "assistant", "content": sample.AssistantMessage},
            },
        }
        
        data, _ := json.Marshal(openaiFormat)
        writer.Write(data)
        writer.WriteString("\n")
        count++
    }
    
    writer.Flush()
    log.Printf("Generated %d training samples", count)
}
```

## 下一步

1. **集成到 Pipeline**：在 `pipeline_process.go` 中添加快照记录
2. **实现持久化**：将快照保存到文件或数据库
3. **构建数据集工具**：编写脚本批量处理快照
4. **可视化工具**：Web UI 查看上下文演变

## 注意事项

1. **性能影响**：`Export()` 会复制所有数据，建议只在需要时调用
2. **队列读取**：从 `contextQueue` 读取消息是非阻塞的，可能丢失正在传输的消息
3. **并发安全**：所有读取操作都加了锁，但快照本身不是线程安全的
4. **存储空间**：每个快照可能有几 KB 到几十 KB，注意磁盘空间

## 示例：完整的数据收集流程

```go
// 1. 在 Session 初始化时创建 ContextManager
func NewSession(...) *Session {
    s := &Session{...}
    s.contextMgr = NewContextManager(s)
    return s
}

// 2. 在 Pipeline 处理时记录快照
func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
    before := p.session.contextMgr.Export()
    
    // ... LLM 调用 ...
    
    after := p.session.contextMgr.Export()
    
    // 保存到文件
    saveSnapshot(before, after, userText, response)
}

// 3. 定期导出数据集
func exportDataset() {
    samples := loadAllSamples("dataset/training_samples.jsonl")
    
    // 过滤：只保留有效样本
    filtered := filterSamples(samples, func(s TrainingSample) bool {
        return len(s.AssistantMessage) > 0 && 
               strings.Contains(s.AssistantMessage, "@{")
    })
    
    // 转换格式
    for _, sample := range filtered {
        openaiFormat := ConvertToOpenAIFormat(sample)
        appendToJSONL("dataset/openai_format.jsonl", openaiFormat)
    }
}
```

现在你可以清晰地看到上下文管理的全过程，方便造数据集了！
