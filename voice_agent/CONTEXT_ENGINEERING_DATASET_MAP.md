# Context Engineering 全景图（数据集导向，重构后集中版）

本文目标只有一个：把当前 Voice Agent 的上下文工程按“代码真实结构”集中讲清楚，便于你做数据集。  
不改运行逻辑，不讲可视化。

## 0）先看这 4 个文件（现在已集中）

1. `agent/context_engine_pipeline.go`：Pipeline 里的 context 路由、回灌、需求更新、冲突处理、记忆上送  
2. `agent/context_engine_prompt.go`：系统提示词统一拼装（BuildPrompt + Layer1~5）  
3. `agent/context_engine_session.go`：Session 里的 context 状态写入（任务/页面/冲突/引用文件）  
4. `agent/context_manager.go`：context 快照导出（Export/ExportJSON/Diff）

---

## 1）唯一拼装函数（触发入口不是唯一）

唯一拼装函数：
- `ContextManager.BuildPrompt(...)`  

代码锚点：
- `agent/context_engine_prompt.go:68`

触发入口（多入口）：
1. 文本输入路径：`handleTextInput -> startProcessing -> buildFullSystemPrompt(includeContextQueue=true)`  
2. 回灌触发路径：`EnqueueContext -> processContextUpdate -> startProcessing`  
3. 语音 think 路径：`thinkLoop -> runThinkStream -> buildSystemPrompt -> buildFullSystemPrompt(includeContextQueue=false)`  
4. HTTP callback 路径：`HandleServiceCallback -> IngestContextFromCallback -> enqueueContextMessage`

代码锚点：
- `agent/session_ws.go:90`
- `agent/pipeline_process.go:13`
- `agent/context_engine_pipeline.go:21`
- `agent/context_engine_pipeline.go:71`
- `agent/pipeline_process.go:181`
- `agent/pipeline_process.go:244`
- `agent/pipeline_process.go:265`
- `agent/context_engine_prompt.go:60`
- `agent/http.go:174`
- `agent/http.go:235`
- `agent/context_engine_pipeline.go:77`

---

## 2）状态存放（State Pool）

状态源不是一个 struct，而是 `Session + Pipeline + History`：

- 需求：`Session.Requirements`
- 任务：`Session.OwnedTasks`、`Session.ActiveTaskID`、`Session.ViewingPageID`
- 冲突问题：`Session.PendingQuestions`
- 对话历史：`Pipeline.history`
- 消息缓存：`Pipeline.contextQueue`、`Pipeline.highPriorityQueue`、`Pipeline.pendingContexts`

代码锚点：
- `agent/session_types.go:43`
- `agent/session_types.go:64`
- `agent/session_types.go:69`
- `agent/session_types.go:73`
- `agent/pipeline.go:64`
- `agent/pipeline.go:65`
- `agent/pipeline.go:67`

---

## 3）写入路径（谁在改 context）

### A. 用户输入写入（WS）
- `text_input` 触发主处理
- `page_navigate` 改 activeTask/viewingPage
- `add_reference_files` 改 requirements.reference_files

代码锚点：
- `agent/session_ws.go:10`
- `agent/session_ws.go:90`
- `agent/context_engine_session.go:12`
- `agent/context_engine_session.go:123`

### B. 模型动作写入（LLM Action）
- token 解析：`parser.Feed`
- 动作执行：`executeAction -> executor.Execute`
- 回灌：callback 到 `EnqueueContext`

代码锚点：
- `agent/pipeline_process.go:73`
- `agent/pipeline_process.go:275`
- `agent/pipeline_process.go:309`
- `agent/pipeline_process.go:333`
- `agent/context_engine_pipeline.go:21`

### C. callback 写入（外部服务）
- `HandleServiceCallback` 组装 `ContextMessage`
- 统一走 `IngestContextFromCallback`

代码锚点：
- `agent/http.go:174`
- `agent/http.go:235`
- `agent/context_engine_pipeline.go:77`

### D. EnqueueContext 分流
- `requirements_updated`：立即 `handleRequirementsUpdate`（不排队）
- `task_list_update`：更新 task 并通知前端
- 其他：按优先级进入 `highPriorityQueue/contextQueue`
- idle 下尝试主动触发一轮 `processContextUpdate`

代码锚点：
- `agent/context_engine_pipeline.go:21`
- `agent/context_engine_pipeline.go:23`
- `agent/context_engine_pipeline.go:29`
- `agent/context_engine_pipeline.go:44`
- `agent/context_engine_pipeline.go:59`
- `agent/context_engine_pipeline.go:71`

### E. 高优先级通道
- `highPriorityListener` 处理 `conflict_question/system_notify`
- 冲突问题写入 `PendingQuestions`
- 中断重试失败会降级到 `pendingContexts`

代码锚点：
- `agent/context_engine_pipeline.go:132`
- `agent/context_engine_pipeline.go:163`
- `agent/context_engine_pipeline.go:183`
- `agent/context_engine_pipeline.go:213`

### F. 需求更新落地
- JSON -> Requirements 字段
- `RefreshCollectedFields` + 状态切换 (`collecting/ready`)
- 发送 `requirements_progress/requirements_summary`

代码锚点：
- `agent/context_engine_pipeline.go:226`
- `agent/context_engine_pipeline.go:280`
- `agent/context_engine_pipeline.go:290`
- `agent/context_engine_pipeline.go:298`

---

## 4）读取路径（Prompt 如何消费 context）

统一入口：
- `buildFullSystemPrompt(...) -> BuildPrompt(...)`

固定五层：
1. Layer1: base prompt / requirements mode
2. Layer2: task list
3. Layer3: pending questions
4. Layer4: context messages
5. Layer5: protocol instructions

代码锚点：
- `agent/context_engine_prompt.go:60`
- `agent/context_engine_prompt.go:68`
- `agent/context_engine_prompt.go:98`
- `agent/context_engine_prompt.go:111`
- `agent/context_engine_prompt.go:137`
- `agent/context_engine_prompt.go:159`
- `agent/context_engine_prompt.go:10`

重要条件：
- 只有 `includeContextQueue=true` 时才读取 Layer4（drain queue）
- `runThinkStream` 走的是 `buildSystemPrompt(...false)`，所以 think 路径默认不拼 Layer4

代码锚点：
- `agent/context_engine_prompt.go:85`
- `agent/pipeline_process.go:265`
- `agent/pipeline_process.go:244`

---

## 5）历史、冲突与记忆（数据集必须覆盖）

历史写入：
- `startProcessing` 开始写 user
- `runThinkStream` 也会写 user（增量输入）
- 正常完成写 assistant
- 打断写 `AddInterruptedAssistant`

代码锚点：
- `agent/pipeline_process.go:16`
- `agent/pipeline_process.go:245`
- `agent/pipeline_process.go:154`
- `agent/pipeline.go:261`

冲突处理：
- `tryResolveConflict` 读取 pending questions 并回写反馈

代码锚点：
- `agent/context_engine_pipeline.go:355`

记忆上送：
- `maybeCompressHistory`（长历史切片异步上送）
- `pushRemainingContext`（会话关闭时上送剩余）

代码锚点：
- `agent/context_engine_pipeline.go:409`
- `agent/context_engine_pipeline.go:443`
- `agent/session.go:52`
- `agent/session.go:57`

---

## 6）Export 快照语义（做标注要特别注意）

- `ContextManager.Export()` 不是纯只读：会从 `contextQueue` 非阻塞读取（drain）
- `BuildPrompt` 读 `pendingContexts + contextQueue`，但不会清空 `pendingContexts`
- 清空 `pendingContexts` 的逻辑在 `drainContextQueue`（测试/辅助）

代码锚点：
- `agent/context_manager.go:85`
- `agent/context_manager.go:141`
- `agent/context_engine_prompt.go:159`
- `agent/context_engine_pipeline.go:100`
- `agent/context_engine_pipeline.go:107`

---

## 7）数据集采集最小闭环（全路径统一 schema）

围绕每次主轮次（`startProcessing`）采集四段：

1. `state_before`
- requirements 全量
- task pool + activeTask + viewingPage
- pendingQuestions
- pendingContexts 长度
- history 快照

2. `model_input`
- final `systemPrompt`
- `userText`
- `source_path`（`text_input/context_update/think/callback`）

3. `model_output`
- raw token stream
- parsed actions
- visible text（主流程是 ProtocolFilter；think 输出来自 Parser.VisibleText）

4. `state_after`
- requirements 新状态
- task/pendingQuestion 变化
- 新产出的 ContextMessage

建议统一样本格式：
- `state_before + model_input -> model_output -> state_after`

---

## 8）阅读顺序（按集中结构）

1. `agent/context_engine_pipeline.go`
2. `agent/context_engine_prompt.go`
3. `agent/context_engine_session.go`
4. `agent/context_manager.go`
5. `agent/pipeline_process.go`
6. `agent/http.go`
