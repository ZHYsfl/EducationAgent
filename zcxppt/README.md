# zcxppt — 多模态 AI 教学 PPT Agent 后端服务

`zcxppt` 是 EducationAgent 项目中负责「PPT 任务流转、实时反馈处理、页面渲染与导出」的 Go 后端服务。使用 Gin 框架构建，支持 Redis/内存双仓储模式、内置 LLM 运行时（Moonshot/Kimi）、Python-pptx 子进程渲染、OSS 对象存储适配。

---

## 1. 系统架构总览

```
POST /api/v1/ppt/init
    │
    ├─ Create Task (taskRepo) → InitCanvas (N pages, pptRepo)
    ├─ Query KB Service → 获取 RAG 知识摘要
    ├─ LLM (kimi-k2.5) generates python-pptx code per page
    │      ↓
    ├─ Renderer (subprocess): exec python-pptx code → .pptx file → upload to OSS
    │      ↓
    └─ UpdatePageCode: each page's render_url = signed OSS URL ✅

POST /api/v1/ppt/feedback
    │
    ├─ HandleVADEvent: resolve suspend + process pending queue
    ├─ RunFeedbackLoop: LLM merge with tool calling
    │     ├─ modify     → 修改指定页面
    │     ├─ insert_before  → 在目标页前插入新页
    │     ├─ insert_after   → 在目标页后插入新页
    │     ├─ delete     → 删除指定页面
    │     ├─ global_modify → 全局修改所有页面
    │     └─ reorder    → 页面重排
    ├─ Renderer: exec merged code → new .pptx → upload OSS
    └─ UpdatePageCode with new render_url ✅

POST /api/v1/ppt/export (format=pptx)
    │
    ├─ Fetch all page py_codes from pptRepo
    ├─ Generate Python merge script with embedded page codes
    ├─ Run: produces single multi-page .pptx
    └─ Upload to OSS → signed download URL ✅

POST /api/v1/ppt/generate_pages (batch)
    │
    ├─ Resolve target pages (specific or all canvas pages)
    ├─ RunFeedbackLoop concurrently (goroutine pool, maxParallel)
    └─ UpdatePageCode + Render for each page ✅

GET /api/v1/ppt/export/{export_id}
    │
    └─ Returns download_url when completed ✅
```

---

## 2. 技术栈与依赖

| 组件 | 选型 |
|---|---|
| 语言 | Go `1.25+` |
| Web 框架 | `gin-gonic/gin` |
| 缓存/存储 | `redis/go-redis/v9` |
| LLM SDK | `openai/openai-go/v3` |
| Tool Calling | `tool_calling_go` (Moonshot/Kimi 兼容) |
| PPT 渲染 | Python `python-pptx` (子进程执行) |
| 对象存储 | 本地文件 / 阿里云 OSS / MinIO |
| 环境变量 | `joho/godotenv` |
| UUID | `google/uuid` |

---

## 3. 目录结构

```
zcxppt/
├── cmd/server/main.go
├── internal/
│   ├── config/config.go
│   ├── contract/
│   │   ├── codes.go          ← 统一错误码常量
│   │   └── response.go      ← 统一 HTTP 响应封装
│   ├── http/
│   │   ├── router.go        ← Gin 路由注册（含健康检查）
│   │   ├── middleware/
│   │   │   └── auth_middleware.go
│   │   └── handlers/
│   │       ├── task_handler.go
│   │       ├── ppt_handler.go
│   │       ├── feedback_handler.go
│   │       └── export_handler.go
│   ├── infra/
│   │   ├── llm/tool_runtime.go    ← LLM 运行时（真实/Mock）
│   │   ├── oss/client.go         ← OSS 存储客户端
│   │   └── renderer/
│   │       ├── renderer.go        ← Go 渲染服务（调用 Python）
│   │       └── render_page.py    ← Python 渲染脚本
│   ├── model/
│   │   ├── task.go
│   │   ├── ppt.go
│   │   ├── feedback.go
│   │   └── export.go
│   ├── repository/
│   │   ├── task_repository.go / task_redis_repository.go
│   │   ├── ppt_repository.go / ppt_redis_repository.go
│   │   ├── feedback_repository.go / feedback_redis_repository.go
│   │   └── export_repository.go / export_redis_repository.go
│   └── service/
│       ├── task_service.go
│       ├── ppt_service.go         ← Init + KB 查询 + LLM 生成 + Renderer 调度
│       ├── feedback_service.go     ← 反馈处理（6 种意图 + 解悬挂 + 挂起队列）
│       ├── export_service.go      ← PPTX/DOCX/HTML5 导出
│       ├── merge_service.go
│       └── notify_service.go      ← 通知 Voice Agent
├── tests/
│   ├── blackbox/
│   ├── integration/
│   └── whitebox/
├── .env
├── .env.example
├── go.mod
├── go.sum
└── README.md
```

---

## 4. 配置说明（`.env.example`）

| 变量名 | 用途 | 默认值 |
|---|---|---|
| `ZCXPPT_PORT` | 服务监听端口 | `9400` |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `REDIS_PASSWORD` | Redis 密码 | 空 |
| `REDIS_DB` | Redis 库编号 | `0` |
| `INTERNAL_KEY` | 内部调用鉴权密钥 | （必填） |
| `JWT_SECRET` | JWT 签名密钥 | `change-me` |
| `VOICE_AGENT_URL` | 通知上游服务地址 | `http://localhost:9200` |
| `TASK_REPO_MODE` | Task 仓储模式 | `redis` |
| `PPT_REPO_MODE` | PPT 仓储模式 | `redis` |
| `FEEDBACK_REPO_MODE` | Feedback 仓储模式 | `redis` |
| `EXPORT_REPO_MODE` | Export 仓储模式 | `redis` |
| `LLM_RUNTIME_MODE` | LLM 运行模式 | `real` |
| `LLM_API_KEY` | LLM API Key（`sk-`开头的 Kimi Key） | （必填） |
| `LLM_MODEL` | LLM 模型名 | `kimi-k2.5` |
| `LLM_BASE_URL` | LLM Base URL | `https://api.moonshot.cn/v1` |
| `OSS_PROVIDER` | OSS 提供方 | `local` |
| `OSS_BUCKET` | Bucket 名称 | `exports` |
| `OSS_SECRET_ID` | OSS 密钥 ID | 空 |
| `OSS_SECRET_KEY` | OSS 密钥 Key | 空 |
| `OSS_SIGNING_KEY` | OSS 签名密钥 | 空 |
| `OSS_BASE_URL` | OSS 基础地址 | `http://localhost:9000` |
| `OSS_LOCAL_PATH` | 本地存储路径 | `./data/oss` |
| `RENDERER_MODE` | 渲染模式（`real`/`mock`） | `real` |
| `PYTHON_PATH` | Python 解释器路径 | `python` |
| `RENDER_SCRIPT_PATH` | Python 渲染脚本路径 | `./internal/infra/renderer/render_page.py` |
| `RENDER_DIR` | 临时渲染目录 | `./data/renders` |
| `RENDER_URL_PREFIX` | OSS URL 前缀 | 空 |
| `RENDER_TIMEOUT_SEC` | 渲染超时秒数 | `60` |

---

## 5. API 完整接口

基础前缀：`/api/v1`，全部入站路由统一经过内部鉴权中间件（`INTERNAL_KEY`）。

### 5.1 PPT 核心接口

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/ppt/init` | 初始化 PPT 任务，触发 LLM 生成 + 渲染 |
| `POST` | `/ppt/feedback` | 提交反馈（支持 6 种意图） |
| `POST` | `/ppt/generate_pages` | 批量生成页面（并发控制） |
| `GET` | `/canvas/status?task_id=` | 查询画布状态（页面路由表） |
| `POST` | `/canvas/vad-event` | 通知 VAD 事件（解悬挂） |
| `GET` | `/ppt/page/:page_id/render?task_id=` | 查询单页渲染结果 |

### 5.2 任务管理接口

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/tasks` | 创建任务元数据 |
| `GET` | `/tasks/:task_id` | 查询任务详情 |
| `PUT` | `/tasks/:task_id/status` | 更新任务状态 |
| `GET` | `/tasks?session_id=&page=&page_size=` | 按会话分页查询任务列表 |
| `GET` | `/tasks/:task_id/preview` | 获取 PPT 预览数据 |

### 5.3 导出接口

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/ppt/export` | 创建导出任务 |
| `GET` | `/ppt/export/:export_id` | 查询导出进度/下载链接 |

### 5.4 内部调度接口

| 方法 | 路径 | 说明 |
|---|---|---|
| `POST` | `/internal/feedback/timeout_tick` | 触发悬挂超时轮询处理 |

### 5.5 健康检查接口（无需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/health` | 返回 `{"code":200,"service":"zcxppt"}` |
| `GET` | `/ready` | 返回 `{"code":200,"message":"ready"}` |

---

## 6. 意图（Intent）ActionType 枚举

| 值 | 语义 | 目标页面 |
|---|---|---|
| `modify` | 修改指定页面内容 | `target_page_id` 指定 |
| `insert_before` | 在目标页**之前**插入新页 | `target_page_id` 指定 |
| `insert_after` | 在目标页**之后**插入新页 | `target_page_id` 指定 |
| `delete` | 删除指定页面 | `target_page_id` 指定 |
| `global_modify` | 全局修改所有页面 | `target_page_id = "ALL"` |
| `reorder` | 页面重排 | `instruction` 包含目标位置 |
| `resolve_conflict` | 回应冲突提问 | 必须携带 `reply_to_context_id` |

---

## 7. 关键流程说明

### 7.1 PPT 初始化（`/ppt/init`）

1. 创建 Task（状态 `generating`）
2. 在 Redis 中初始化 Canvas（生成 N 个 page）
3. 查询 KB Service 获取 RAG 知识摘要（`Subject` 字段已传入）
4. 调用 Kimi k2.5 LLM，生成每页的 `python-pptx` 代码
5. Renderer 子进程执行 Python 代码 → 生成单页 `.pptx` 文件
6. 上传至 OSS，获取签名下载 URL
7. 更新每页 `render_url`，任务状态置为 `completed`

### 7.2 反馈处理（`/ppt/feedback`）

- **解悬挂优先**：若页面处于 `suspended_for_human` 状态，`resolve_conflict` 意图先解悬挂，然后递归处理待处理队列
- **6 种意图路由**：`modify`/`insert_before`/`insert_after`/`delete`/`global_modify`/`reorder`
- **冲突保护**：LLM 返回 `ask_human` 时，页面挂起，通过 `notify_service` 通知 Voice Agent
- **渲染后更新**：每次合并成功后立即重新渲染，更新 `render_url`

### 7.3 悬挂超时处理（`/internal/feedback/timeout_tick`）

- 每 45 秒内调用一次
- 查找已过期的 `suspend` 记录：
  - 重试次数 < 3 → 重发 TTS 询问（`RetryCount++`）
  - 重试次数 ≥ 3 → LLM 自决策（自动合并），解除悬挂

### 7.4 批量生成（`/ppt/generate_pages`）

- 支持指定 `page_ids` 或生成全部画布页面
- 并发控制：`max_parallel` 参数，默认 4
- goroutine pool 模式，结果按顺序写入响应数组

---

## 8. 快速启动

```bash
# 1. 安装依赖
go mod tidy

# 2. 准备环境变量
cp .env.example .env
# 编辑 .env，填写：
#   INTERNAL_KEY        (内部调用密钥)
#   LLM_API_KEY        (Kimi API Key: sk-...)
#   LLM_MODEL           (kimi-k2.5)
#   REDIS_ADDR         (Redis 地址)

# 3. 安装 Python 依赖
pip install python-pptx

# 4. 启动服务
go run ./cmd/server
```

---

## 9. 测试

```bash
go test ./...
```

---

## 10. 环境变量快速参考

```
ZCXPPT_PORT=9400
REDIS_ADDR=localhost:6379
INTERNAL_KEY=your-internal-key
LLM_RUNTIME_MODE=real
LLM_API_KEY=sk-GXv6IkzW8BjsmvSebuQKF4iBGIElocIbuIoJu2vicjCPnQI5
LLM_MODEL=kimi-k2.5
LLM_BASE_URL=https://api.moonshot.cn/v1
RENDERER_MODE=real
PYTHON_PATH=python
RENDER_SCRIPT_PATH=./internal/infra/renderer/render_page.py
RENDER_DIR=./data/renders
RENDER_TIMEOUT_SEC=60
```
