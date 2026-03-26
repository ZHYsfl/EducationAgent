# Voice Agent 重构方案：多 Agent 异步协作架构

## 1. 架构设计

### 1.1 Agent 角色定义

**Voice Agent（对话 Agent）**：
- 负责：实时语音交互、需求收集、用户意图理解
- 能力：ASR → LLM → TTS 低延迟管道
- 职责：从对话中提取结构化信息，通过消息总线与其他 Agent 通信

**PPT Agent（任务执行 Agent）**：
- 负责：PPT 生成、页面编辑、冲突解决
- 能力：视觉理解、内容生成、版本管理
- 职责：接收 Voice Agent 的任务请求，报告进度和冲突

**Memory Agent（记忆管理 Agent）**：
- 负责：用户画像、偏好学习、历史记忆
- 能力：信息提取、知识沉淀
- 职责：异步更新用户画像，提供个性化建议

**KB Agent（知识检索 Agent）**：
- 负责：知识库查询、Web 搜索、内容沉淀
- 能力：RAG、向量检索、搜索结果精炼
- 职责：异步提供上下文增强

---

## 2. 通信协议设计

### 2.1 消息格式（符合接口规范）

```go
// AgentMessage 是 Agent 间通信的标准消息格式
type AgentMessage struct {
    MessageID   string          `json:"message_id"`   // msg_xxx
    Type        string          `json:"type"`         // 消息类型
    Sender      string          `json:"sender"`       // voice_agent/ppt_agent/memory_agent/kb_agent
    Receiver    string          `json:"receiver"`     // 目标 Agent
    Priority    string          `json:"priority"`     // high/normal/low
    ContextID   string          `json:"context_id"`   // 关联的上下文 ID
    Timestamp   int64           `json:"timestamp"`    // 毫秒时间戳
    Payload     json.RawMessage `json:"payload"`      // 具体数据
    ReplyTo     string          `json:"reply_to"`     // 回复的消息 ID
}
```

### 2.2 消息类型定义

**Voice Agent → PPT Agent**：
```go
// 任务初始化
{
    "type": "task_init_request",
    "payload": {
        "topic": "高等数学",
        "requirements": {...}
    }
}

// 修改反馈
{
    "type": "feedback_request",
    "payload": {
        "intents": [...]
    }
}
```

**PPT Agent → Voice Agent**：
```go
// 进度更新
{
    "type": "progress_update",
    "payload": {
        "task_id": "task_xxx",
        "status": "generating",
        "progress": 30
    }
}

// 冲突询问（高优先级）
{
    "type": "conflict_question",
    "priority": "high",
    "payload": {
        "context_id": "ctx_xxx",
        "question": "检测到冲突，选择 A 还是 B？"
    }
}
```

---

## 3. Tool Calling 标准化

### 3.1 定义 Tools（替代字符串标记）

```go
// tools.go
package agent

// PPT 相关的 tool definitions
var PPTTools = []ToolDefinition{
    {
        Name: "init_ppt_task",
        Description: "初始化一个新的 PPT 任务",
        Parameters: ToolParameters{
            Type: "object",
            Properties: map[string]PropertySchema{
                "topic": {
                    Type: "string",
                    Description: "PPT 主题",
                },
                "knowledge_points": {
                    Type: "array",
                    Items: &PropertySchema{Type: "string"},
                    Description: "核心知识点列表",
                },
                // ... 其他字段
            },
            Required: []string{"topic"},
        },
    },
    {
        Name: "modify_ppt",
        Description: "修改已有的 PPT 内容",
        Parameters: ToolParameters{
            Type: "object",
            Properties: map[string]PropertySchema{
                "action_type": {
                    Type: "string",
                    Enum: []string{"modify", "insert", "delete", "reorder", "style"},
                },
                "page_id": {
                    Type: "string",
                    Description: "目标页面 ID（可选）",
                },
                "instruction": {
                    Type: "string",
                    Description: "具体修改指令",
                },
            },
            Required: []string{"action_type", "instruction"},
        },
    },
}
```

---

## 4. 实现步骤

### Phase 1: 消息总线基础设施
1. 实现 `AgentMessage` 结构
2. 创建消息路由器（基于现有的 ContextBus 扩展）
3. 支持优先级队列和异步分发

### Phase 2: Tool Calling 集成
1. 定义所有 tool schemas
2. 在 LLM 请求中添加 tools 参数
3. 解析 tool calls 并转换为 AgentMessage
4. 移除字符串标记解析逻辑

### Phase 3: Agent 间异步通信
1. PPT Agent 通过 HTTP POST `/api/v1/voice/ppt_message` 发送消息
2. Voice Agent 接收后入队到 ContextBus
3. 高优先级消息（冲突）立即打断当前对话

### Phase 4: 扩展到其他 Agent
1. Memory Agent 异步提取和更新
2. KB Agent 异步检索和沉淀
3. 所有 Agent 通过统一消息协议通信

---

## 5. 关键优势

1. **真正的并行** - Agent 不互相阻塞
2. **标准化通信** - 符合接口规范，使用 tool calling
3. **可扩展** - 新增 Agent 只需实现消息协议
4. **可测试** - 消息驱动，易于模拟和测试
5. **符合规范** - 遵守团队的接口规范和数据格式

---

## 6. 与 Step-GUI 思路的对应

| Step-GUI 概念 | 我们的实现 |
|---|---|
| 电话 Agent | Voice Agent |
| 电脑 Agent | PPT Agent |
| 消息传递 | AgentMessage + ContextBus |
| 并行执行 | Goroutine + 异步 HTTP |
| 自主编排 | Tool Calling 决策 |
| 小模型优化 | 可选：针对性 fine-tune |

---

## 7. 下一步行动

**建议优先级**：
1. **高优先级**：Tool Calling 标准化（替代字符串标记）
2. **中优先级**：消息协议标准化（AgentMessage）
3. **低优先级**：扩展到 Memory/KB Agent

**是否现在开始重构？**
