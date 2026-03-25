# zcxppt

`zcxppt` 是 EducationAgent 中负责「PPT 任务流转、反馈处理、导出」的后端服务，使用 Go + Gin 构建，支持 Redis/内存仓储切换、LLM 运行时、OSS 存储适配。

---

## 1. 项目做什么

- 提供任务管理 API（创建、查询、列表、状态更新）
- 提供 PPT 初始化与页面渲染查询 API
- 提供反馈提交与超时轮询处理 API
- 提供导出任务创建与导出结果查询 API
- 通过中间件做内部调用鉴权（`INTERNAL_KEY`）

---

## 2. 技术栈与依赖

- Go `1.25.5`
- Web 框架：`gin-gonic/gin`
- 缓存/存储：`redis/go-redis/v9`
- 环境变量：`joho/godotenv`
- UUID：`google/uuid`
- LLM SDK：`openai/openai-go/v3`

---

## 3. 目录结构图（完整）

```text
zcxppt/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── contract/
│   │   ├── codes.go
│   │   └── response.go
│   ├── http/
│   │   ├── router.go
│   │   ├── middleware/
│   │   │   └── auth_middleware.go
│   │   └── handlers/
│   │       ├── task_handler.go
│   │       ├── ppt_handler.go
│   │       ├── feedback_handler.go
│   │       └── export_handler.go
│   ├── infra/
│   │   ├── llm/
│   │   │   └── tool_runtime.go
│   │   └── oss/
│   │       └── client.go
│   ├── model/
│   │   ├── task.go
│   │   ├── ppt.go
│   │   ├── feedback.go
│   │   └── export.go
│   ├── repository/
│   │   ├── task_repository.go
│   │   ├── task_redis_repository.go
│   │   ├── ppt_repository.go
│   │   ├── ppt_redis_repository.go
│   │   ├── feedback_repository.go
│   │   ├── feedback_redis_repository.go
│   │   ├── export_repository.go
│   │   └── export_redis_repository.go
│   └── service/
│       ├── task_service.go
│       ├── ppt_service.go
│       ├── feedback_service.go
│       ├── export_service.go
│       ├── merge_service.go
│       └── notify_service.go
├── tests/
│   ├── blackbox/
│   │   ├── testkit_test.go
│   │   ├── task_api_test.go
│   │   ├── ppt_api_test.go
│   │   ├── feedback_api_test.go
│   │   ├── export_api_test.go
│   │   └── handler_error_test.go
│   ├── integration/
│   │   └── notify_service_test.go
│   └── whitebox/
│       ├── middleware_test.go
│       ├── service_branch_test.go
│       ├── service_error_more_test.go
│       ├── repository_redis_test.go
│       ├── repository_redis_more_test.go
│       ├── repository_extra_branch_test.go
│       ├── feedback_repo_test.go
│       ├── feedback_service_timeout_test.go
│       ├── llm_runtime_test.go
│       └── llm_runtime_real_test.go
├── .env
├── .env.example
├── go.mod
├── go.sum
└── README.md
```

---

## 4. 每个文件具体实现说明

### 4.1 启动入口

- `cmd/server/main.go`  
  程序入口：加载配置、校验配置、初始化 Redis、按模式装配各仓储（Redis/内存）、初始化 OSS/LLM/Service/Handler/中间件，注册路由并启动 Gin 服务。

### 4.2 配置层

- `internal/config/config.go`  
  定义 `Config` 结构体；从 `.env` / 环境变量加载配置；提供字符串与整数环境变量读取函数及默认值兜底。

### 4.3 协议与响应约定

- `internal/contract/codes.go`  
  定义业务返回码/状态码常量，统一服务内部错误语义。
- `internal/contract/response.go`  
  定义统一 HTTP 响应结构与响应封装辅助方法。

### 4.4 HTTP 路由与中间件

- `internal/http/router.go`  
  集中注册 `/api/v1` 下所有路由（tasks、ppt、canvas、internal feedback tick）。
- `internal/http/middleware/auth_middleware.go`  
  实现内部鉴权中间件：校验请求中的内部密钥，拦截未授权调用。

### 4.5 HTTP Handler 层

- `internal/http/handlers/task_handler.go`  
  处理任务相关 API：创建任务、查询任务、更新状态、列表。
- `internal/http/handlers/ppt_handler.go`  
  处理 PPT 相关 API：初始化 PPT、页面渲染状态查询、画布状态查询。
- `internal/http/handlers/feedback_handler.go`  
  处理反馈 API：接收用户反馈、触发/处理反馈超时轮询。
- `internal/http/handlers/export_handler.go`  
  处理导出 API：创建导出任务、查询导出进度/结果。

### 4.6 领域模型层

- `internal/model/task.go`  
  任务领域模型定义（任务标识、状态、时间、关联字段等）。
- `internal/model/ppt.go`  
  PPT 领域模型定义（页面、渲染状态、关联任务信息等）。
- `internal/model/feedback.go`  
  反馈领域模型定义（反馈内容、来源、时间、关联对象等）。
- `internal/model/export.go`  
  导出领域模型定义（导出任务状态、产物地址、错误信息等）。

### 4.7 Repository 抽象与实现

- `internal/repository/task_repository.go`  
  定义 Task 仓储接口，并包含内存版实现（供本地/测试使用）。
- `internal/repository/task_redis_repository.go`  
  Task 的 Redis 实现，含持久化与 TTL 逻辑。

- `internal/repository/ppt_repository.go`  
  定义 PPT 仓储接口，并包含内存版实现。
- `internal/repository/ppt_redis_repository.go`  
  PPT 的 Redis 仓储实现。

- `internal/repository/feedback_repository.go`  
  定义 Feedback 仓储接口，并包含内存版实现。
- `internal/repository/feedback_redis_repository.go`  
  Feedback 的 Redis 仓储实现。

- `internal/repository/export_repository.go`  
  定义 Export 仓储接口，并包含内存版实现。
- `internal/repository/export_redis_repository.go`  
  Export 的 Redis 仓储实现。

### 4.8 Service 业务层

- `internal/service/task_service.go`  
  封装任务主流程业务：创建、查询、状态流转与校验。
- `internal/service/ppt_service.go`  
  封装 PPT 生命周期业务：初始化、页面/画布状态处理。
- `internal/service/feedback_service.go`  
  封装反馈处理流程：接收反馈、调用 LLM、通知上游、超时策略处理。
- `internal/service/export_service.go`  
  封装导出业务：创建导出任务、写入导出信息、返回导出结果。
- `internal/service/merge_service.go`  
  封装内容合并/结果聚合逻辑（供 PPT/反馈等流程复用）。
- `internal/service/notify_service.go`  
  封装对外通知能力（调用 `VOICE_AGENT_URL`）。

### 4.9 基础设施适配层

- `internal/infra/llm/tool_runtime.go`  
  LLM 运行时封装：支持真实模式调用模型、工具调用/返回处理，以及测试可替代运行逻辑。
- `internal/infra/oss/client.go`  
  OSS 客户端封装：统一上传/URL 生成/对象访问接口，屏蔽具体存储实现差异。

### 4.10 测试（blackbox / integration / whitebox）

#### blackbox（按 API 黑盒验证）

- `tests/blackbox/testkit_test.go`  
  黑盒测试公共初始化与辅助方法（测试服务启动、请求构造、通用断言）。
- `tests/blackbox/task_api_test.go`  
  任务 API 全流程黑盒测试。
- `tests/blackbox/ppt_api_test.go`  
  PPT API 黑盒测试。
- `tests/blackbox/feedback_api_test.go`  
  反馈 API 黑盒测试。
- `tests/blackbox/export_api_test.go`  
  导出 API 黑盒测试。
- `tests/blackbox/handler_error_test.go`  
  Handler 级错误路径黑盒测试（参数错误、状态错误、异常分支）。

#### integration（集成测试）

- `tests/integration/notify_service_test.go`  
  `notify_service` 与外部交互流程的集成测试。

#### whitebox（白盒单测/分支覆盖）

- `tests/whitebox/middleware_test.go`  
  鉴权中间件白盒测试。
- `tests/whitebox/service_branch_test.go`  
  Service 层多分支路径测试。
- `tests/whitebox/service_error_more_test.go`  
  Service 层错误分支与边界场景增强测试。
- `tests/whitebox/repository_redis_test.go`  
  Redis 仓储基础行为测试。
- `tests/whitebox/repository_redis_more_test.go`  
  Redis 仓储更多分支和异常路径测试。
- `tests/whitebox/repository_extra_branch_test.go`  
  Repository 额外分支覆盖测试。
- `tests/whitebox/feedback_repo_test.go`  
  Feedback 仓储逻辑白盒测试。
- `tests/whitebox/feedback_service_timeout_test.go`  
  Feedback 超时策略与轮询处理测试。
- `tests/whitebox/llm_runtime_test.go`  
  LLM runtime 的 mock/非真实模式测试。
- `tests/whitebox/llm_runtime_real_test.go`  
  LLM runtime 在真实模式配置下的行为测试。

### 4.11 根目录文件

- `.env.example`  
  环境变量模板文件（无敏感值），用于快速复制本地配置。
- `.env`  
  本地运行配置文件（可能包含密钥，不应提交）。
- `go.mod`  
  模块定义、主依赖声明及本地 `replace`（如 `tool_calling_go`、`educationagent/oss`）。
- `go.sum`  
  依赖完整性校验记录。
- `README.md`  
  项目说明文档（本文件）。

---

## 5. 配置说明（基于 `.env.example`）

| 变量名 | 用途 | 默认值 |
|---|---|---|
| `ZCXPPT_PORT` | 服务监听端口 | `9400` |
| `REDIS_ADDR` | Redis 地址 | `localhost:6379` |
| `REDIS_PASSWORD` | Redis 密码 | 空 |
| `REDIS_DB` | Redis 库编号 | `0` |
| `INTERNAL_KEY` | 内部调用鉴权密钥（必填） | 无 |
| `JWT_SECRET` | JWT 签名密钥 | `change-me` |
| `VOICE_AGENT_URL` | 通知上游服务地址 | `http://localhost:9200` |
| `TASK_REPO_MODE` | Task 仓储模式（`redis`/`memory`） | `redis` |
| `TASK_TTL_HOURS` | Task TTL 小时数（Redis） | `168` |
| `PPT_REPO_MODE` | PPT 仓储模式 | `redis` |
| `FEEDBACK_REPO_MODE` | Feedback 仓储模式 | `redis` |
| `EXPORT_REPO_MODE` | Export 仓储模式 | `redis` |
| `LLM_RUNTIME_MODE` | LLM 运行模式 | `real` |
| `LLM_API_KEY` | LLM Key（real 模式必填） | 无 |
| `LLM_MODEL` | LLM 模型名（real 模式必填） | `moonshot-v1-8k` |
| `LLM_BASE_URL` | LLM Base URL（real 模式必填） | `https://api.moonshot.cn/v1` |
| `OSS_PROVIDER` | OSS 提供方（如 local） | `local` |
| `OSS_BUCKET` | Bucket 名称 | `exports` |
| `OSS_REGION` | OSS 区域 | 空 |
| `OSS_SECRET_ID` | OSS 密钥 ID | 空 |
| `OSS_SECRET_KEY` | OSS 密钥 Key | 空 |
| `OSS_SIGNING_KEY` | OSS 签名密钥 | 空 |
| `OSS_BASE_URL` | OSS 基础地址 | `http://localhost:9000` |
| `OSS_LOCAL_PATH` | 本地存储路径 | `./data/oss` |

---

## 6. 快速启动

1) 安装依赖

```bash
go mod tidy
```

2) 准备环境变量

```bash
cp .env.example .env
```

然后编辑 `.env`，至少填好：`INTERNAL_KEY`、Redis 配置，若 `LLM_RUNTIME_MODE=real` 还需填写 LLM 三件套。

3) 运行服务

```bash
go run ./cmd/server
```

---

## 7. API 总览

基础前缀：`/api/v1`

### 7.1 PPT 相关接口（含实现位置）

- `POST /api/v1/ppt/init`  
  接收已确认的结构化需求（如 `teaching_elements` + `description`），创建 `task_id` 并启动首稿生成流程。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`ppt.POST("/init", ...)`）
  - Handler：`internal/http/handlers/ppt_handler.go` -> `func (h *PPTHandler) Init(...)`
  - Service：`internal/service/ppt_service.go` -> `Init(...)`

- `POST /api/v1/ppt/feedback`  
  接收 Voice Agent 的 `intents`，执行页面修改/插入/删除/全局修改；冲突场景进入合并与挂起处理流程。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`ppt.POST("/feedback", ...)`）
  - Handler：`internal/http/handlers/feedback_handler.go` -> `func (h *FeedbackHandler) Feedback(...)`
  - Service：`internal/service/feedback_service.go` -> `Handle(...)`

- `GET /api/v1/canvas/status?task_id={task_id}`  
  返回任务的页面路由表与页面状态，供前端轮询预览。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`v1.GET("/canvas/status", ...)`）
  - Handler：`internal/http/handlers/ppt_handler.go` -> `func (h *PPTHandler) CanvasStatus(...)`
  - Service：`internal/service/ppt_service.go` -> `GetCanvasStatus(...)`

- `POST /api/v1/ppt/export`  
  提交导出请求（`pptx/docx/html5`），返回 `export_id`。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`ppt.POST("/export", ...)`）
  - Handler：`internal/http/handlers/export_handler.go` -> `func (h *ExportHandler) Create(...)`
  - Service：`internal/service/export_service.go` -> `Create(...)`

- `GET /api/v1/ppt/export/{export_id}`  
  查询导出状态，完成后返回下载地址（如 `download_url` 字段）。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`ppt.GET("/export/:export_id", ...)`）
  - Handler：`internal/http/handlers/export_handler.go` -> `func (h *ExportHandler) Get(...)`
  - Service：`internal/service/export_service.go` -> `Get(...)`

- `GET /api/v1/ppt/page/{page_id}/render?task_id={task_id}`  
  返回单页渲染结果及版本信息，便于前端页级刷新和调试。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`ppt.GET("/page/:page_id/render", ...)`）
  - Handler：`internal/http/handlers/ppt_handler.go` -> `func (h *PPTHandler) PageRender(...)`
  - Service：`internal/service/ppt_service.go` -> `GetPageRender(...)`

- （调用职责）`POST /api/v1/voice/ppt_message`  
  用于冲突/关键状态变化时通知 Voice Agent，冲突问题应包含 `context_id`。  
  **实现说明：**该接口**不在本仓库提供路由**，是本服务作为客户端去调用外部 Voice Agent。  
  **实现位置：**
  - `internal/service/notify_service.go`（封装外呼）
  - `internal/service/feedback_service.go`（在反馈流程中触发通知）
  - 目标地址来源：`VOICE_AGENT_URL`

### 7.2 Tasks 接口（任务元数据，与 PPT 状态联动）

- `POST /api/v1/tasks`  
  创建任务元数据。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`tasks.POST("", ...)`）
  - Handler：`internal/http/handlers/task_handler.go` -> `Create(...)`
  - Service：`internal/service/task_service.go` -> `CreateTask(...)`

- `GET /api/v1/tasks/{task_id}`  
  查询任务详情与状态。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`tasks.GET("/:task_id", ...)`）
  - Handler：`internal/http/handlers/task_handler.go` -> `Get(...)`
  - Service：`internal/service/task_service.go` -> `GetTask(...)`

- `PUT /api/v1/tasks/{task_id}/status`  
  更新任务状态（如 `pending/generating/completed/failed/exporting`）。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`tasks.PUT("/:task_id/status", ...)`）
  - Handler：`internal/http/handlers/task_handler.go` -> `UpdateStatus(...)`
  - Service：`internal/service/task_service.go` -> `UpdateTaskStatus(...)`

- `GET /api/v1/tasks?session_id={session_id}&page=1&page_size=20`  
  按会话分页查询任务列表。  
  **实现位置：**
  - 路由注册：`internal/http/router.go`（`tasks.GET("", ...)`）
  - Handler：`internal/http/handlers/task_handler.go` -> `List(...)`
  - Service：`internal/service/task_service.go` -> `ListTasks(...)`

> 全部入站路由统一经过 `internal/http/middleware/auth_middleware.go` 的内部鉴权中间件。

---

## 8. 测试命令

```bash
go test ./...
```

---

