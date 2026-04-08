# zcxppt 智能教学 PPT Agent 后端服务设计说明

> 本文档详细描述 zcxppt 项目的系统架构、接口设计、内部逻辑、核心算法及关键设计决策。

---

## 一、项目概述

**zcxppt** 是 EducationAgent 项目中负责「PPT 任务流转、实时反馈处理、页面渲染与导出」的 Go 后端服务（内部代号 PPT Agent）。

### 1.1 核心能力


| 能力           | 说明                                                                   |
| ------------ | -------------------------------------------------------------------- |
| **PPT 智能生成** | 基于用户输入的主题、描述、知识点，通过 LLM（Kimi k2.5）生成多页 python-pptx 代码，并自动渲染为 PPTX 文件 |
| **实时反馈处理**   | 接收 Voice Agent 转发的用户语音反馈，通过意图解析路由到 6 种操作（修改/插入/删除/全局修改/重排/生成内容）      |
| **知识库融合**    | 调用 KB Service（RAG）获取知识摘要，作为 LLM 生成的内容背景                              |
| **多格式参考融合**  | 支持上传 PDF/DOCX/PPTX/图片/视频参考文件，通过格式专用解析器提取文字和样式信息并注入 LLM               |
| **教案自动生成**   | 基于 PPT 主题和教学要素，自动生成结构化 Word 教案（.docx）                                |
| **内容多样性**    | 生成教学动画（HTML5/CSS3）和互动小游戏（选择题/连连看/排序/填空）                              |
| **多格式导出**    | 支持将完整 PPT 导出为 PPTX / DOCX / HTML5                                    |
| **VAD 事件联动** | 监听 Voice Agent 的 VAD（语音活动检测）事件，自动解除悬挂页面冲突并继续处理待处理反馈队列                |


### 1.2 典型调用链路

```
Voice Agent（语音交互前端）
    │
    ├── POST /api/v1/ppt/init
    │       → 创建 Task → KB 查询 → LLM 生成多页代码
    │       → Python 渲染（并发） → OSS 上传 → 返回 task_id
    │
    ├── GET /canvas/status?task_id=
    │       → 返回画布页面路由表和渲染状态
    │
    ├── POST /api/v1/ppt/feedback
    │       → 意图解析（可选）→ 冲突检测/悬挂 → LLM 合并
    │       → 渲染更新 → 通知 Voice Agent（TTS/下一步）
    │
    ├── POST /canvas/vad-event
    │       → 解悬挂 → 处理 pending 队列 → 继续反馈流程
    │
    ├── POST /internal/ppt/teaching_plan
    │       → LLM 生成教案 JSON → Python 渲染 DOCX → OSS 上传
    │
    ├── POST /internal/ppt/content_diversity
    │       → LLM 生成动画/游戏 HTML5 → 可选导出 GIF/MP4 → OSS 上传
    │
    └── POST /internal/ppt/export
            → 收集所有页面代码 → Python 合并导出 → OSS 上传 → 返回下载链接
```

---

## 二、技术架构

### 2.1 技术栈


| 层级      | 选型                                                      |
| ------- | ------------------------------------------------------- |
| 语言      | Go 1.25+                                                |
| Web 框架  | gin-gonic/gin                                           |
| 缓存/持久化  | Redis（go-redis/v9）；支持内存兜底                               |
| LLM SDK | openai/openai-go/v3 + tool_calling_go（Moonshot/Kimi 兼容） |
| PPT 渲染  | Python python-pptx（子进程执行）                               |
| 对象存储    | 本地文件 / 阿里云 OSS / MinIO（通过 `educationagent/oss` 统一抽象）    |
| 环境配置    | joho/godotenv                                           |
| UUID    | google/uuid                                             |


### 2.2 分层架构

```
┌─────────────────────────────────────────────────────────┐
│                    HTTP 层（handlers）                    │
│  PPTHandler / FeedbackHandler / ExportHandler           │
│  TeachingPlanHandler / ContentDiversityHandler          │
├─────────────────────────────────────────────────────────┤
│                    Service 层（业务逻辑）                │
│  PPTService / FeedbackService / IntentParser            │
│  RefFusionService / MergeService / NotifyService       │
│  ExportService / TeachingPlanService                     │
│  ContentDiversityService                                │
├─────────────────────────────────────────────────────────┤
│                    Repository 层（数据访问）              │
│  TaskRepository / PPTRepository / FeedbackRepository    │
│  ExportRepository                                       │
│  （均支持内存模式 ↔ Redis 模式，按配置切换）            │
├─────────────────────────────────────────────────────────┤
│                    Infrastructure 层（外部依赖）          │
│  LLM ToolRuntime / OSS Client / FileParser             │
│  Renderer（Python子进程） / KB Tool / NotifyService    │
├─────────────────────────────────────────────────────────┤
│                    Contract 层（契约）                   │
│  统一响应格式（APIResponse）/ HTTP 状态码定义           │
└─────────────────────────────────────────────────────────┘
```

### 2.3 核心设计原则

1. **依赖注入**：所有 Service 的外部依赖（仓储、渲染器、LLM、OSS）均通过构造函数或 Attach 方法注入，支持 Mock 测试
2. **双仓储模式**：每个 Repository 同时提供内存实现（InMemory*）和 Redis 实现（Redis*），通过环境变量切换，开发/测试无需 Redis
3. **并发安全**：所有内存存储均使用 `sync.RWMutex`；PPT 渲染使用信号量控制最大并发数（默认 8）
4. **异步非阻塞**：LLM 生成、渲染、教案生成、内容多样性导出均为异步 goroutine，通过轮询接口或回调通知获取结果
5. **悬挂-冲突机制**：当 LLM 无法自动合并时，通过悬挂状态暂停当前页面，等待用户明确回复（VAD 事件触发）

---

## 三、目录结构

```
zcxppt/
├── cmd/server/main.go              # 程序入口，依赖注入编排
├── go.mod / go.sum                # Go 模块定义
├── .env.example                   # 环境变量示例
│
├── internal/
│   ├── config/config.go           # 配置加载（从环境变量读取）
│   │
│   ├── contract/                   # API 契约层
│   │   ├── response.go            # 统一响应 Success/Error 封装
│   │   └── codes.go               # HTTP 状态码常量（200/40001/40400/50000...）
│   │
│   ├── model/                      # 数据模型层
│   │   ├── task.go                # Task 任务模型
│   │   ├── ppt.go                 # PPT 初始化/画布/VAD/批量生成模型
│   │   ├── feedback.go             # Intent/FeedbackRequest/Suspend/Pending 模型
│   │   ├── teaching_plan.go        # 教案/内容多样性模型（复用部分也在此）
│   │   └── export.go               # 导出任务模型
│   │
│   ├── repository/                 # 数据访问层
│   │   ├── task_repository.go     # Task 仓储接口 + 内存实现
│   │   ├── ppt_repository.go      # PPT 画布仓储接口 + 内存实现
│   │   ├── feedback_repository.go  # Feedback 悬挂/待处理队列仓储
│   │   ├── export_repository.go   # 导出任务仓储
│   │   ├── task_redis_repository.go
│   │   ├── ppt_redis_repository.go
│   │   ├── feedback_redis_repository.go
│   │   └── export_redis_repository.go
│   │
│   ├── service/                    # 业务逻辑层
│   │   ├── ppt_service.go          # PPT 初始化核心逻辑
│   │   ├── feedback_service.go     # 反馈处理核心逻辑（6种意图）
│   │   ├── intent_parser.go        # 自然语言意图解析（LLM/关键词双模式）
│   │   ├── ref_fusion_service.go   # 参考文件融合（解析+抽取+样式）
│   │   ├── merge_service.go        # 三路合并（基础/当前/新版本）
│   │   ├── notify_service.go       # 通知 Voice Agent（TTS/事件）
│   │   ├── export_service.go       # PPTX/DOCX/HTML 导出
│   │   ├── teaching_plan_service.go # 教案生成（LLM→JSON→DOCX）
│   │   └── content_diversity_service.go # 动画/游戏生成+导出
│   │
│   ├── http/                       # HTTP 层
│   │   ├── router.go               # Gin 路由注册
│   │   ├── handlers/                # 请求处理器
│   │   │   ├── ppt_handler.go      # Init / CanvasStatus / PageRender / VADEvent
│   │   │   ├── feedback_handler.go  # Feedback / GeneratePages / TickTimeout
│   │   │   ├── export_handler.go    # Export 创建/查询
│   │   │   ├── teaching_plan_handler.go # 教案生成/状态
│   │   │   └── content_diversity_handler.go # 动画/游戏/导出/集成
│   │   └── middleware/
│   │       └── auth_middleware.go  # X-Internal-Key 鉴权
│   │
│   └── infra/                      # 基础设施层
│       ├── llm/tool_runtime.go     # LLM 工具调用运行时
│       ├── oss/client.go           # OSS 存储客户端
│       ├── reference_file_parser.go # 多格式文件解析（PDF/DOCX/PPTX/图片/视频）
│       ├── kb_tool.go             # 知识库查询工具（独立 Tool/Agent）
│       └── renderer/
│           ├── renderer.go          # 渲染服务（Go 调用 Python）
│           ├── render_page.py       # 单页 PPTX 渲染脚本
│           ├── render_animation.py  # 动画导出 GIF/MP4 脚本
│           ├── render_game.py       # 游戏相关脚本
│           └── render_teaching_plan.py # 教案渲染脚本
│
└── data/
    ├── renders/                    # 临时渲染输出目录
    └── oss/                        # 本地 OSS 模式存储目录
```

---

## 四、接口设计详解

### 4.1 对外 API（/api/v1，仅 4 个）

以下 4 个接口为外部公开接口，供 Voice Agent 或其他上游服务调用，全部通过 `X-Internal-Key` 请求头鉴权。

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/ppt/init` | 初始化 PPT 任务 |
| POST | `/api/v1/ppt/feedback` | 提交用户反馈 |
| GET | `/api/v1/canvas/status?task_id=` | 查询画布状态（页面路由表+渲染状态） |
| POST | `/api/v1/canvas/vad-event` | VAD 事件通知（解悬挂） |

> **注意**：`/internal` 前缀下的所有接口均为内部调度接口，不对外开放。`/health` 和 `/ready` 为健康检查接口（无需鉴权），不计入外部接口。

### 4.2 内部调度接口（/internal，仅限内部调用）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/internal/feedback/generate_pages` | 批量生成多页 |
| POST | `/internal/feedback/timeout_tick` | 悬挂超时处理（内部定时调用） |
| GET | `/internal/ppt/page/:page_id/render` | 查询单页渲染结果 |
| POST | `/internal/ppt/export` | 创建导出任务 |
| GET | `/internal/ppt/export/:export_id` | 查询导出进度/下载链接 |
| POST | `/internal/ppt/teaching_plan` | 触发起床教案生成 |
| GET | `/internal/ppt/teaching_plan/:plan_id` | 查询教案生成状态 |
| POST | `/internal/ppt/content_diversity` | 触发动画/游戏生成 |
| GET | `/internal/ppt/content_diversity/:result_id` | 查询内容多样性生成状态 |
| POST | `/internal/ppt/content_diversity/export` | 导出动画/游戏为 GIF/MP4 |
| POST | `/internal/ppt/integrate` | 将动画/游戏嵌入 PPT 页面 |


---

### 4.2 POST /api/v1/ppt/init — PPT 初始化

#### 请求体（`PPTInitRequest`）

```json
{
  "user_id": "string（必填）",
  "session_id": "string（必填）",
  "topic": "string（必填）",
  "description": "string（必填）",
  "total_pages": 5,
  "subject": "数学",
  "audience": "初中二年级",
  "global_style": "简洁专业",
  "teaching_elements": {
    "knowledge_points": ["知识点1", "知识点2"],
    "teaching_goals": ["目标1"],
    "teaching_logic": "从概念到应用",
    "key_difficulties": ["难点1"],
    "duration": "45分钟",
    "interaction_design": "课堂问答",
    "output_formats": ["pptx"]
  },
  "reference_files": [
    {
      "file_id": "string",
      "file_url": "string",
      "file_type": "pdf|docx|pptx|image|video",
      "instruction": "请提取与此教学主题相关的内容"
    }
  ],
  "auto_generate_teaching_plan": true,
  "auto_generate_content_diversity": true,
  "content_diversity_type": "both|animation|game",
  "content_diversity_game_type": "quiz|matching|ordering|fill_blank|random",
  "content_diversity_animation_style": "slide_in|fade|zoom|draw|pulse|all"
}
```

#### 响应体

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "task_id": "task_xxxxxxxx-xxxx-xxxx",
    "status": "processing"  // "processing" 或 "completed"
  }
}
```

#### 内部处理流程

```
1. 参数校验
   ├─ user_id / session_id / topic / description 必须非空
   └─ reference_files 格式校验（file_id / file_url / file_type / instruction 均不能为空）

2. 创建任务
   ├─ taskRepo.Create(Task{SessionID, Topic, Status:"generating", Progress:10})
   └─ pptRepo.InitCanvas(taskID, totalPages)
       → 生成 N 个 page_id（UUID），创建 PageOrder 数组
       → 初始化每页 status="rendering"

3. 启动异步生成（goroutine）
   │
   ├─ 3.1 KB 查询
   │   └─ POST {KB_SERVICE_URL}/api/v1/kb/parse
   │       → 返回摘要文本（key: summary/answer/content/text/documents）
   │       → 失败不阻断（fallback 空字符串），通知 Voice Agent "知识库未成功"
   │
   ├─ 3.2 参考文件融合
   │   └─ RefFusionService.Fuse(req.ReferenceFiles, topic)
   │       → 按 file_type 并发解析（PDF→OCR/转写, DOCX→转写, PPTX→ZIP解析+样式抽取,
   │         Image→OCR, Video→关键帧+字幕）
   │       → LLM 定向抽取：只保留与当前反馈意图相关的内容片段
   │       → PPTX 额外提取：主题色/字体/版式 → StyleGuide
   │       → KB 补充：TopicHints 知识要点
   │
   ├─ 3.3 LLM 生成多页代码
   │   └─ PPTService.generateInitialPages()
   │       → System Prompt：融合 KB摘要 + 参考内容 + 样式指南 + python-pptx 规范
   │       → 注册 kb_query 工具（LLM 可主动查询知识库）
   │       → 期望返回 JSON：{"pages":[{"title":"...","py_code":"..."}]}
   │       → 去 markdown 代码块 → JSON 解析
   │
   ├─ 3.4 并发渲染（goroutine pool，semaphore=8）
   │   └─ Renderer.Render(ctx, PyCode, RenderConfig)
   │       → 构造 JSON payload → stdin 传入 Python 脚本
   │       → 执行 python render_page.py → 生成 .pptx 文件
   │       → 上传到 OSS → 返回签名下载 URL
   │       → pptRepo.UpdatePageCode(taskID, pageID, pyCode, renderURL)
   │
   └─ 3.5 联动生成（与 PPT 生成解耦，并发执行）
       ├─ AutoGenerateTeachingPlan=true → TeachingPlanService.Generate()
       └─ AutoGenerateContentDiversity=true → ContentDiversityService.Generate()
```

---

### 4.3 GET /api/v1/canvas/status — 画布状态查询

#### 请求参数

`?task_id=task_xxxxxxxx`

#### 响应体

```json
{
  "code": 200,
  "data": {
    "task_id": "task_xxx",
    "page_order": ["page_uuid1", "page_uuid2", "page_uuid3"],
    "current_viewing_page_id": "page_uuid1",
    "pages_info": [
      {
        "page_id": "page_uuid1",
        "status": "completed",  // rendering | completed | failed | suspended_for_human
        "last_update": 1710000000000,
        "render_url": "https://oss.example.com/renders/xxx.pptx?sign=..."
      }
    ]
  }
}
```

#### 内部逻辑

直接委托给 `pptRepo.GetCanvasStatus(taskID)`，对每个页面 status 做标准化（normalizePageStatus）：非标状态统一返回 `"rendering"`。

---

### 4.4 POST /api/v1/ppt/feedback — 用户反馈处理

#### 请求体（`FeedbackRequest`）

```json
{
  "task_id": "string（必填）",
  "viewing_page_id": "string（必填）",
  "base_timestamp": 1710000000000,
  "raw_text": "把这一页的标题改成'函数的导数'",
  "reply_to_context_id": "ctx_xxx（仅 resolve_conflict 时必填）",
  "intents": [
    {
      "action_type": "modify|insert_before|insert_after|delete|global_modify|reorder|generate_animation|generate_game",
      "target_page_id": "page_uuid（modify/insert/delete/reorder 时填）",
      "instruction": "原始修改指令",
      "animation_style": "slide_in|fade|zoom|draw|pulse|all（generate_animation时）",
      "game_type": "quiz|matching|ordering|fill_blank（generate_game时）"
    }
  ],
  "reference_files": [...],
  "topic": "导数",
  "subject": "数学",
  "kb_summary": "..."
}
```

#### 响应体

```json
{
  "code": 200,
  "data": {
    "accepted_intents": 1,
    "queued": false,
    "content_diversity_results": [
      {
        "intent_index": 0,
        "action_type": "generate_animation",
        "result_id": "div_xxx",
        "status": "generating"
      }
    ]
  }
}
```

#### 内部处理流程

```
1. 参数校验
   ├─ task_id / viewing_page_id / base_timestamp / raw_text 必须非空
   ├─ 若 intents 为空 → IntentParser.ParseWithContext() 从 raw_text 解析
   │   → LLM 结构化解析（优先）→ 关键词兜底
   └─ action_type 合法性校验（9种）

2. 冲突场景检测
   ├─ feedbackRepo.GetSuspend(taskID, viewingPageID)
   │   → 若页面处于悬挂状态（suspended=true）：
   │       ├─ 若意图包含 resolve_conflict：
   │       │   ├─ 校验 reply_to_context_id == suspend.ContextID
   │       │   ├─ feedbackRepo.ResolveSuspend() → 解除悬挂
   │       │   ├─ 从待处理队列取出一个 pending → 递归 Handle()
   │       │   └─ 返回 {accepted: N, queued: false}
   │       └─ 否则（新反馈进入排队）：
   │           ├─ feedbackRepo.EnqueuePending() → 加入 FIFO 队列
   │           ├─ 通知 Voice Agent "conflict_question" + TTS 文字（暂停）
   │           └─ 返回 {accepted: N, queued: true}

3. 正常意图处理（遍历每个 Intent）
   │
   ├─ modify：修改指定页面
   │   ├─ 获取目标页面 current PyCode
   │   ├─ 参考文件重融合（RefFusionService.FuseForFeedback）
   │   ├─ llmRuntime.RunFeedbackLoop(req, current)
   │   │   → System: "你是PPT反馈合并编排器，结合参考资料进行融合"
   │   │   → 注册 emit_merge_result 工具
   │   │   → 期望 LLM 返回：{merge_status:"auto_resolved"/"ask_human", merged_pycode/question_for_user}
   │   ├─ applyMergeResult():
   │   │   ├─ ask_human → SetSuspend → 通知 Voice Agent 冲突问题
   │   │   └─ auto_resolved → UpdatePageCode → 渲染 → 通知 Voice Agent
   │
   ├─ insert_before / insert_after：插入新页面
   │   ├─ generateNewPageCode() → LLM 生成新页 PyCode
   │   ├─ 渲染新页面 → 获取 renderURL
   │   └─ pptRepo.InsertPageBefore/After()
   │
   ├─ delete：删除页面
   │   └─ pptRepo.DeletePage()
   │
   ├─ global_modify：全局修改所有页面
   │   ├─ 遍历 canvas.PageOrder
   │   └─ 对每个页面执行 modify 逻辑
   │
   ├─ reorder：页面重排
   │   ├─ 解析指令格式："page_abc→2" 或 "把page_abc移到第2页"
   │   ├─ 获取页面数据 → 删除原位置 → 插入目标位置
   │   └─ pptRepo.InsertPageAfter/Before()
   │
   └─ generate_animation / generate_game：内容多样性生成
       ├─ ContentDiversityService.Generate() → 返回 result_id
       └─ 通知 Voice Agent "content_diversity_started"

4. 返回处理摘要
```

---

### 4.5 POST /api/v1/canvas/vad-event — VAD 事件处理

#### 请求体（`VADEventRequest`）

```json
{
  "task_id": "string（必填）",
  "viewing_page_id": "string（必填）",
  "timestamp": 1710000000000
}
```

#### 内部逻辑

当用户开始说话（VAD 检测到语音活动）时，Voice Agent 通知本服务：

```
1. 参数校验（task_id / page_id / timestamp）
2. 校验页面存在
3. 更新 current_viewing_page_id = viewingPageID
4. 查询该页面是否处于悬挂状态
   ├─ 是 → ResolveSuspend() 解除悬挂
   │      → 通知 Voice Agent "vad_resolved" + "好的，请说"
   │      → 从 pending 队列取出队首 → 递归调用 FeedbackService.Handle()
   └─ 否 → 无操作（静默忽略）
```

这是实现「语音对话中随时打断/继续」的关键机制：用户说话时任何页面冲突自动解除，待处理反馈自动继续。

---

### 4.6 POST /internal/feedback/generate_pages — 批量页面生成

#### 请求体

```json
{
  "task_id": "string",
  "base_timestamp": 1710000000000,
  "raw_text": "把所有页面背景改成蓝色",
  "intents": [{"action_type": "global_modify", "instruction": "..."}],
  "page_ids": ["page_uuid1", "page_uuid2"],  // 可选：指定页面；空则全部
  "max_parallel": 4,                          // 并发数，默认4
  "reference_files": [...]
}
```

#### 内部逻辑

使用 goroutine pool（semaphore 控制并发）并发处理多个页面的 LLM 合并和渲染，最终返回每个页面的结果数组。

---

### 4.7 POST /internal/feedback/timeout_tick — 悬挂超时处理

由 main.go 中的 `startTimeoutTicker` goroutine 每 45 秒调用一次：

```
1. feedbackRepo.ListExpiredSuspends(now)
   → 查找所有 ExpiresAt <= now 且 Resolved=false 的悬挂记录

2. 对每个过期记录：
   ├─ RetryCount < 3：
   │   → RetryCount++，重置 ExpiresAt = now + 45s
   │   → 重新发送 "conflict_question" TTS 提示
   └─ RetryCount >= 3：
       → ResolveSuspend() → 解除悬挂
       → 通知 Voice Agent "conflict_timeout"
       → UpdatePageStatus(task, page, "failed", "超时已自动跳过")
       → 从 pending 队列取一个处理
```

---

### 4.8 POST /internal/ppt/export — 导出 PPT

#### 请求体

```json
{
  "task_id": "string",
  "format": "pptx|docx|html|html5"
}
```

#### 导出格式说明

**PPTX 导出**（`exportPPTX`）：

1. 从 pptRepo 获取所有页面 PyCode，构建 JSON `{"page_id": "py_code"}`
2. 构造 Python 合并脚本（字符串拼接，无 heredoc），内嵌所有页面代码
3. 每个页面对应一个 `slide`，通过 `exec()` 执行页面 PyCode
4. 错误页面显示错误信息而不中断
5. 保存 → 上传 OSS → 返回签名 URL

**DOCX 导出**（`exportDOCX`）：
生成 Word 文档，每页作为标题+代码块段落，包含导出时间和任务 ID。

**HTML 导出**（`exportHTML`）：
生成单文件 HTML，每页为一个 `<div class="slide">`，支持键盘/鼠标导航、淡入动画、进度条。

---

### 4.9 POST /internal/ppt/teaching_plan — 教案生成

#### 请求体

```json
{
  "task_id": "xxx",
  "topic": "二次函数的图像与性质",
  "subject": "数学",
  "description": "面向初三学生...",
  "audience": "初中三年级",
  "duration": "45分钟",
  "teaching_elements": {...},
  "style_guide": "..."
}
```

#### 响应体

```json
{
  "code": 200,
  "data": {
    "task_id": "task_xxx",
    "plan_id": "plan_xxx",
    "status": "generating"
  }
}
```

#### 内部逻辑（异步 goroutine）

```
1. 生成唯一 plan_id = "plan_" + UUID
2. 存储 job 到内存 map（planRepo）
3. goroutine:
   ├─ TeachingPlanService.generatePlanContent()
   │   └─ LLM 生成 JSON 教案（title/subject/teaching_goals/
   │      teaching_process/warm_up/new_teaching/practice/summary/homework/
   │      classroom_activities/teaching_reflection/teaching_aids）
   │
   └─ TeachingPlanService.renderDOCX()
       └─ Python 生成 .docx
           ├─ 设置页边距、标题样式（蓝底白字）
           ├─ 教学目标（项目符号列表）
           ├─ 教学重点/难点
           ├─ 教学方法
           ├─ 教学过程（热身→导入→新授→练习→总结→作业）
           ├─ 课堂活动设计
           └─ 教学反思
       └─ 上传 OSS → 返回签名 URL
```

---

### 4.10 POST /internal/ppt/content_diversity — 内容多样性生成

#### 请求体

```json
{
  "task_id": "xxx",
  "topic": "二次函数",
  "subject": "数学",
  "type": "both|animation|game",
  "game_type": "quiz|matching|ordering|fill_blank|random",
  "animation_style": "slide_in|fade|zoom|draw|pulse|all",
  "kb_summary": "..."
}
```

#### 动画生成（generateAnimations）

LLM System Prompt 要求生成 2-3 个不同风格的 HTML5 动画，风格包括：

- **slide_in**：滑入动画
- **fade**：淡入淡出
- **zoom**：缩放动画
- **draw**：绘制/描边动画
- **pulse**：脉冲/心跳动画

每个动画包含完整的 `<!DOCTYPE html>` 结构，内嵌 CSS3 `@keyframes` + JS 循环播放逻辑，背景色 `#f0f4f8`，主色调 `#1F4E79`。

#### 游戏生成（generateGames）

支持 4 种游戏类型：

- **quiz**：4 选 1 选择题，答对绿色鼓励，答错红色+正确答案
- **matching**：连连看词汇匹配
- **ordering**：排序题
- **fill_blank**：填空题

游戏界面包含开始按钮、题目展示、交互反馈、得分显示，风格统一为蓝色标题栏。

#### 导出流程

- **HTML5**（`ExportAnimation`/`ExportGame`）：
  - 若已有 HTMLContent → 直接上传 OSS → 返回签名 URL
- **GIF/MP4**（`exportToGIFMP4`）：
  - 将 HTML 内容写入临时文件
  - 调用 Python 脚本 `render_animation.py`（通过 stdin JSON 传入参数）
  - Python 使用 selenium/playwright 或 imageio 方式将 HTML 动画逐帧捕获
  - 生成的文件上传 OSS → 回调 Voice Agent

---

### 4.11 POST /internal/ppt/integrate — 内容嵌入 PPT

将动画/游戏的内容描述（HTML URL）以注释形式嵌入目标页面 PyCode，并在 PPT 备注区域添加提示。便于后续导出 PPTX 时在附页展示对应 HTML 内容的二维码或链接。

---

## 五、数据模型详解

### 5.1 Task（任务）

```go
type Task struct {
    TaskID              string    // "task_" + UUID，全局唯一
    SessionID           string    // 所属会话
    Topic               string    // PPT 主题
    Status              string    // pending / generating / completed / failed
    Progress            int       // 0~100 进度
    CurrentPageID       string    // 当前浏览页面
    PlanID              string    // 关联教案任务 ID
    ContentResultID     string    // 关联内容多样性任务 ID
    CreatedAt/UpdatedAt time.Time
}
```

### 5.2 Canvas / Page（画布与页面）

```
Canvas（pptRepo）
  └─ TaskID
  └─ PageOrder: []string{pageID1, pageID2, ...}    // 页面顺序
  └─ CurrentViewingPageID: string                   // 当前浏览页
  └─ PagesInfo: []PageStatusInfo                    // 每页状态摘要

Page（pptRepo 每 Task 一个独立 map）
  └─ TaskID / PageID / Status / RenderURL / PyCode / Version / UpdatedAt
```

页面 PyCode 存储 LLM 生成的 python-pptx 代码文本，每次修改后版本号递增。

### 5.3 Feedback 核心模型

**Intent（意图）**：

```go
type Intent struct {
    ActionType     string  // modify/insert_before/insert_after/delete/
                        // global_modify/reorder/resolve_conflict/
                        // generate_animation/generate_game
    TargetPageID   string  // 目标页面 ID
    Instruction    string  // 原始用户指令
    AnimationStyle string  // generate_animation 时使用
    GameType       string  // generate_game 时使用
    ResultID       string  // 内容多样性生成后填充
}
```

**SuspendState（悬挂状态）**：

```go
type SuspendState struct {
    TaskID/PageID/ContextID   // 唯一标识
    Question       string     // 向用户提出的冲突问题
    RetryCount     int       // 重试次数（≥3 自动解除）
    ExpiresAt      int64     // 过期时间戳（毫秒）
    CreatedAt      int64
    Resolved       bool       // 是否已解决
}
```

**PendingFeedback（待处理反馈队列）**：
FIFO 队列，当页面处于悬挂状态时，新反馈入队；悬挂解除后队首出队并继续处理。

### 5.4 FusionResult（参考融合结果）

```go
type FusionResult struct {
    ExtractedContent []ExtractedFileContent  // 每个文件的解析+抽取结果
    StyleGuide       StyleGuide             // 颜色/字体/版式
    TopicHints       []string               // KB 补充知识
}

type StyleGuide struct {
    ThemeColors []string          // ["#1F4E79", "#4472C4"]
    Fonts       []string          // ["Microsoft YaHei", "SimHei"]
    Layouts     []string          // ["title+content", "two-column"]
    ColorHex    map[string]string // {"bg": "#f0f4f8", ...}
}
```

---

## 六、Service 层核心逻辑详解

### 6.1 PPTService — PPT 初始化编排

**职责**：协调 KB 查询 → 参考文件融合 → LLM 多页生成 → 并发渲染 → 联动子服务。

**关键设计点**：

- **KB 降级策略**：KB 查询失败时，仅记录日志并 fallback 为空字符串，不阻断主流程。同时通知 Voice Agent 提示用户。
- **并发渲染池**：使用 `semaphore = 8` 的 channel 控制最大并发渲染数，避免子进程过多导致系统资源耗尽。
- **LLM KB 工具注册**：在 `generateInitialPages` 时注册 `kb_query` 工具，允许 LLM 在生成过程中主动查询知识库补充内容（hot query），而非仅依赖 Init 阶段的 KB 摘要。
- **联动生成解耦**：教案和内容多样性的生成均在独立 goroutine 中执行，与 PPT 主体生成完全解耦，不影响 PPT 完成时间。
- **样式融合注入**：StyleGuide 通过 prompt 中的 `[样式指南]` 节注入到 LLM System Prompt，LLM 在生成 PyCode 时应尽量遵循指定的主色调和字体。

### 6.2 FeedbackService — 反馈处理编排

**职责**：处理用户口语反馈的 6 种意图操作，包含冲突检测、悬挂管理、LLM 合并、渲染更新。

**关键设计点**：

- **悬挂优先策略**：无论收到何种意图，若页面处于悬挂状态，`resolve_conflict` 必须先被处理才能继续其他操作。
- **冲突问题重发机制**：同一冲突问题最多重发 3 次（第 45 秒/第 90 秒/第 135 秒），3 次无回复后自动解除悬挂并继续。
- **参考资料重融合**：每次 `modify`/`global_modify` 都会重新调用 `RefFusionService.FuseForFeedback`，根据当前反馈指令重新抽取相关内容片段，而非仅使用 Init 阶段的结果。
- **MergeResult 双状态**：
  - `auto_resolved`：LLM 成功合并，立即渲染更新页面
  - `ask_human`：LLM 遇到冲突（如同页并发修改），暂停等待用户明确选择，通过 TTS 询问用户

### 6.3 IntentParser — 自然语言意图解析

**职责**：将用户口语指令（raw_text）解析为结构化 Intent 数组。

**双模式设计**：

- **LLM 模式**（优先）：调用 LLM 结构化输出（`ChatCompletionStructured`），利用 LLM 强大的语义理解能力，识别多意图、模糊引用（第2页/当前页）、内容多样性指令。
- **关键词兜底**（备选）：无 LLM 配置时使用关键词匹配，支持：
  - 动画检测：动画/animat/动效/特效/过渡动画 + 风格关键词
  - 游戏检测：游戏/quiz/答题/测验/互动/答题器/选择题/匹配/排序/填空
  - 全局修改：全部/所有页面/全局/每一页
  - 删除：删除/删掉
  - 插入：在...前插入/在...后插入
  - 重排：移到/移动/调换/调整顺序

### 6.4 RefFusionService — 参考文件融合

**职责**：解析多格式参考文件，提取文字内容和样式信息，供 LLM 融合使用。

**格式解析策略**：


| 类型   | 解析方式                              | 提取内容               |
| ---- | --------------------------------- | ------------------ |
| PDF  | 转写服务或内置文本提取（正则匹配括号内容）             | 文本段落               |
| DOCX | 转写服务（需配置 `TRANS_BASE_URL`）        | 文本内容               |
| PPTX | 内置 Python ZIP/XML 解析（无转写服务时）或转写服务 | 文本 + 主题色 + 字体 + 版式 |
| 图片   | OCR 服务（需配置 `OCR_BASE_URL`）        | 图片内文字              |
| 视频   | 转写服务（关键帧 + 音频字幕提取）                | 字幕文本 + 场景描述        |


**定向抽取**：`extractRelevantContent` 方法使用 LLM 从原始文本中仅抽取与当前指令相关的内容片段，避免大量无关上下文污染 LLM 窗口。

### 6.5 LLM ToolRuntime — 反馈合并运行时

**职责**：封装 LLM 的工具调用能力，驱动反馈合并流程。

**emit_merge_result 工具**：
LLM 必须精确调用一次此工具，参数：

- `merge_status`：`"auto_resolved"` 或 `"ask_human"`（必填）
- `merged_pycode`：合并后的 Python 代码（`auto_resolved` 时必填）
- `question_for_user`：冲突问题（TTS 播报给用户，`ask_human` 时必填）

**参考上下文注入**：通过 `buildReferenceContext` 将参考文件摘要、样式指南、知识补充拼接为 prompt 文本段，LLM 在合并时参考这些内容。

### 6.6 MergeService — 三路合并

```go
// base: 初始版本, current: 用户A修改版, incoming: 用户B修改版
ThreeWayMerge(base, current, incoming):
  - base == current → incoming 覆盖（无修改）
  - current == incoming → 相同，无冲突
  - 三者皆不同 → ask_human（需要用户选择）
  - 其他 → current + incoming 拼接
```

（注：当前代码中 `applyMergeResult` 直接使用 LLM 的 `MergedPyCode`，未调用 MergeService 的三路合并逻辑，三路合并目前作为备用方案保留。）

### 6.7 NotifyService — Voice Agent 通知

**职责**：向 Voice Agent 推送 PPT Agent 的状态变化事件（TTS 播报、页面更新、冲突询问、生成完成等）。

**通知事件类型**：


| 事件类型                          | 使用场景      | 典型 TTS 文字           |
| ----------------------------- | --------- | ------------------- |
| `ppt_status`                  | PPT 状态变更  | "正在生成课件"            |
| `conflict_question`           | 页面冲突询问    | LLM 生成的冲突问题         |
| `vad_resolved`                | VAD 解悬挂   | "好的，请说"             |
| `conflict_timeout`            | 冲突超时      | "页面冲突问题超时未回复，已自动跳过" |
| `content_diversity_started`   | 动画/游戏开始生成 | "正在为您生成动画，请稍候"      |
| `content_diversity_completed` | 动画/游戏生成完成 | "动画创意已生成完成"         |
| `content_diversity_failed`    | 动画/游戏生成失败 | 错误信息                |


### 6.8 ExportService — 多格式导出

**异步导出流程**：

1. 创建 ExportJob（status=queued → generating）
2. goroutine 执行导出（PPTX/DOCX/HTML）
3. Python 脚本生成文件 → 上传 OSS
4. 更新 ExportJob（status=completed, downloadURL=OSS签名链接）

**PPTX 合并策略**：将所有页面 PyCode 以 JSON 形式嵌入 Python 脚本，逐页 exec()，任一页面错误不影响其他页面（try-except 隔离）。

---

## 七、Repository 层设计

### 7.1 双模式架构

每个数据域（Task/PPT/Feedback/Export）均定义 Repository 接口，配套提供：

- `NewInMemory*Repository`：内存 Map 实现，用于开发/测试
- `NewRedis*Repository`：Redis Hash/List 实现，用于生产环境

通过环境变量（`*_REPO_MODE=redis|inmemory`）在 main.go 中选择注入。

### 7.2 PPTRepository 核心接口

```go
type PPTRepository interface {
    InitCanvas(taskID string, totalPages int)        // 初始化 N 个空页面
    GetCanvasStatus(taskID string)                   // 获取页面顺序+状态
    SetCurrentViewingPageID(taskID, pageID string)    // 更新当前浏览页
    GetPageRender(taskID, pageID string)             // 获取单页代码+URL
    UpdatePageCode(taskID, pageID, pyCode, url)      // 更新代码+URL（版本递增）
    UpdatePageStatus(taskID, pageID, status, err)    // 更新状态（如 failed）
    InsertPageAfter(taskID, afterID string, newPage)  // 插入页面
    InsertPageBefore(taskID, beforeID string, newPage)
    DeletePage(taskID, pageID string)                // 删除页面
    GetTaskIDByPageID(pageID string)                  // 通过 pageID 反查 taskID
}
```

### 7.3 FeedbackRepository 核心接口

```go
type FeedbackRepository interface {
    EnqueuePending(taskID, pageID string, item)      // 待处理队列入队
    DequeuePending(taskID, pageID string)             // FIFO 出队（取队首）
    ListPending(taskID, pageID string)                // 查看队列（不消费）
    SetSuspend(state)                                 // 设置悬挂
    GetSuspend(taskID, pageID)                        // 查询悬挂（布尔+suspend对象）
    ResolveSuspend(taskID, pageID string)             // 解除悬挂（Resolved=true）
    ListExpiredSuspends(now time.Time)                // 查找所有过期悬挂
}
```

Key 构造方式：`taskID + ":" + pageID`，保证同一 Task 下不同页面完全隔离。

---

## 八、配置体系

### 8.1 环境变量配置（config.go）


| 分类        | 变量                   | 说明                     | 默认值                                                      |
| --------- | -------------------- | ---------------------- | -------------------------------------------------------- |
| **服务**    | `ZCXPPT_PORT`        | 监听端口                   | 9400                                                     |
|           | `INTERNAL_KEY`       | 内部鉴权密钥                 | （必填）                                                     |
| **Redis** | `REDIS_ADDR`         | Redis 地址               | localhost:6379                                           |
|           | `REDIS_PASSWORD`     | Redis 密码               | 空                                                        |
|           | `REDIS_DB`           | Redis 库                | 0                                                        |
| **仓储模式**  | `*_REPO_MODE`        | redis/inmemory         | redis                                                    |
|           | `TASK_TTL_HOURS`     | Task 过期时间              | 168h（7天）                                                 |
| **LLM**   | `LLM_API_KEY`        | Kimi API Key           | （必填）                                                     |
|           | `LLM_MODEL`          | 模型名                    | kimi-k2.5                                                |
|           | `LLM_BASE_URL`       | API 地址                 | [https://api.moonshot.cn/v1](https://api.moonshot.cn/v1) |
|           | `LLM_RUNTIME_MODE`   | real/mock              | real                                                     |
| **知识库**   | `KB_SERVICE_URL`     | KB 查询服务                | localhost:9100                                           |
|           | `KBToolURL`          | LLM 可调用的 KB 工具地址       | 空                                                        |
| **文件解析**  | `OCR_BASE_URL`       | 图片 OCR 服务              | 空                                                        |
|           | `TRANS_BASE_URL`     | 文件转写服务（PDF/DOCX/Video） | 空                                                        |
| **OSS**   | `OSS_PROVIDER`       | local/aliyun/minio     | local                                                    |
|           | `OSS_LOCAL_PATH`     | 本地存储路径                 | ./data/oss                                               |
|           | `OSS_BUCKET`         | Bucket 名               | exports                                                  |
| **渲染**    | `RENDERER_MODE`      | real/mock              | real                                                     |
|           | `PYTHON_PATH`        | Python 解释器             | python                                                   |
|           | `RENDER_SCRIPT_PATH` | 渲染脚本                   | ./internal/infra/renderer/render_page.py                 |
|           | `RENDER_DIR`         | 临时目录                   | ./data/renders                                           |
|           | `RENDER_TIMEOUT_SEC` | 渲染超时                   | 60s                                                      |
| **通知**    | `VOICE_AGENT_URL`    | Voice Agent 回调地址       | localhost:9200                                           |


### 8.2 启动检查（validateConfig）

main.go 在启动时强制校验：

- `REDIS_ADDR` 非空
- `ZCXPPT_PORT` > 0
- `INTERNAL_KEY` 非空

---



