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
| update_requirements | `@{update_requirements\|字段:值}` | 更新课件需求信息 |
| ppt_init | `@{ppt_init\|desc:描述}` | 初始化 PPT 任务（需先收集完所有必填信息） |
| ppt_mod | `@{ppt_mod\|task:任务ID\|page:页面ID\|action:操作\|ins:指令}` | 修改 PPT |
| kb_query | `@{kb_query\|q:查询内容}` | 查询知识库 |
| web_search | `@{web_search\|query:搜索关键词}` | 网络搜索 |

### 必填字段（12个）
在调用 ppt_init 前必须通过 update_requirements 收集：
- topic - 主题
- subject - 学科/专业
- audience - 目标听众
- total_pages - 总页数
- knowledge_points - 知识点
- teaching_goals - 教学目标
- teaching_logic - 教学逻辑
- key_difficulties - 重难点
- duration - 时长
- global_style - 全局风格
- interaction_design - 互动设计
- output_formats - 输出格式

## 示例

```
用户: "帮我做个高等数学的PPT"
LLM: 好的。@{update_requirements|topic:高等数学|subject:数学} 请问目标听众是谁？

用户: "大学生"
LLM: 明白了。@{update_requirements|audience:大学生} 需要多少页？

（收集完所有必填信息后）
LLM: 现在开始制作。@{ppt_init|desc:大学生微积分课程}

让我查询相关资料。@{kb_query|q:微积分基础概念}
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
