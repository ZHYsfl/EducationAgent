# Interactive ReAct 协议规范

## 概述

Voice Agent 使用自定义文本协议实现 Interactive ReAct 模式，支持：
- **Reasoning** (`#{...}`) - 内部推理
- **Acting** (`@{...}`) - 异步动作
- **Interaction** (文本) - 用户对话

## 协议格式

### 动作标记
```
@{type|key:value|key:value}
```

### 思考标记（可选）
```
#{思考内容}
```

## 支持的动作

### Voice Agent 动作

| 动作 | 格式 | 说明 |
|------|------|------|
| ppt_init | `@{ppt_init\|topic:主题\|desc:描述}` | 初始化 PPT 任务 |
| ppt_mod | `@{ppt_mod\|task:任务ID\|page:页面ID\|action:操作\|ins:指令}` | 修改 PPT |
| kb_query | `@{kb_query\|q:查询内容}` | 查询知识库 |

## 示例

```
#{用户想做PPT}好的，我来帮您创建。@{ppt_init|topic:AI|desc:人工智能介绍}

让我查询相关资料。@{kb_query|q:AI发展历程}
```

## 实现架构

### 核心组件

1. **Protocol Parser** (`internal/protocol`)
   - 流式解析 `@{...}` 和 `#{...}`
   - 正则表达式：`@\{([^}]+)\}`

2. **Async Executor** (`internal/executor`)
   - Fire-and-forget 执行
   - 结果通过 ContextMessage 注入

3. **Message Bus** (`internal/bus`)
   - 事件驱动架构
   - 支持 pub/sub

### 执行流程

```
LLM 流式输出
    ↓
Parser 解析协议
    ↓
Executor 异步执行
    ↓
结果 → ContextQueue
    ↓
主动推送（session 空闲时）
```

## 主动推送机制

当异步结果到达时：
1. 检查 session 状态
2. 如果空闲，触发 LLM 处理
3. LLM 决定是否通知用户

不再等待用户下次输入。
