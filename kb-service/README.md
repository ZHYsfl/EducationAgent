# Knowledge Base Service

多模态 AI 教学智能体 — 知识库服务（段怡鹏负责）

默认端口：**9200**

## 负责接口清单（对应规范第 4 章 + 第 8 章）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/kb/collections` | 创建知识库集合 |
| GET  | `/api/v1/kb/collections?user_id=` | 列出用户集合 |
| POST | `/api/v1/kb/documents` | 异步索引文档（外部 URL） |
| POST | `/api/v1/kb/upload` | 文件直传并触发异步索引 |
| GET  | `/api/v1/kb/documents/{doc_id}` | 查询索引进度 |
| DELETE | `/api/v1/kb/documents/{doc_id}` | 删除文档及向量 |
| GET  | `/api/v1/kb/collections/{collection_id}/documents` | 集合内文档列表 |
| POST | `/api/v1/kb/query` | 语义检索（核心 RAG，支持同步/异步回调） |
| POST | `/api/v1/kb/query-chunks` | 关键词 chunk 检索（同步阻塞，供记忆模块 / PPT Agent） |
| POST | `/api/v1/kb/ingest-from-search` | 搜索结果沉淀入库 |
| POST | `/api/v1/kb/parse` | 纯文档解析（不入向量库） |
| GET  | `/health` | 健康检查 |

## 接口交互模式总览

`kb-service` 作为系统的知识库核心服务，完整覆盖了 3 种经典的服务交互模式：

| 接口路径 | 对应模式 | 核心特点 |
|---------|---------|---------|
| `POST /api/v1/kb/query-chunks` | **同步阻塞返回** | 调用方（voice_agent / ppt_agent）发请求后阻塞等待，kb-service 直接返回 `[]chunk` 结果，是最基础的请求-响应模式 |
| `POST /api/v1/kb/query` | **异步 + 主动回调（Webhook）** | 调用方发请求后，kb-service 立即返回 `accepted`，后台异步检索；完成后主动回调 `POST /api/v1/voice/ppt_message`，把结果推送给调用方 |
| `POST /api/v1/kb/ingest-from-search` | **Fire-and-Forget（即发即忘）写入** | web_search 发请求后直接结束，kb-service 后台异步处理写入，无需结果返回，无需回调 |

> 轮询模式（Poll）在本系统中未实现，原因：系统通过 WebSocket 长连接实现 Push，优于轮询的实时性和资源效率。

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `9200` | 服务监听端口 |
| `DATABASE_URL` | 空 | PostgreSQL DSN，例如 `postgres://user:pass@localhost:5432/kbdb?sslmode=disable` |
| `QDRANT_ADDR` | `localhost:6333` | Qdrant 向量库地址 |
| `QDRANT_COLLECTION` | `kb_chunks` | Qdrant collection 名称 |
| `STORAGE_DIR` | `./data/storage` | 本地文件存储根目录 |
| `STORAGE_BASE_URL` | `http://localhost:9200/storage` | 文件访问 URL 前缀 |
| `EMBED_SERVICE_URL` | 空 | Embedding HTTP 服务地址；空时使用 MockEmbedder（非零占位向量，仅开发用）|
| `PYTHON_PARSER_URL` | 空 | Python 解析微服务地址；空时仅支持 text/web_snippet 直接切块 |
| `LLM_API_KEY` | 空 | LLM 密钥；空时禁用 query 意图精化，直接使用原始查询文本 |
| `LLM_MODEL` | `gpt-4o-mini` | LLM 模型名（兼容 OpenAI 接口）|
| `LLM_BASE_URL` | 空 | 自定义 LLM API 地址（如本地部署或第三方兼容接口）|
| `VOICE_CALLBACK_URL` | 空 | Voice Agent 回调地址；异步 query 完成后向此地址发送 kb_result |

## 快速启动（开发模式）

```bash
# 1. 启动依赖
docker run -d --name postgres -e POSTGRES_PASSWORD=pass -e POSTGRES_DB=kbdb -p 5432:5432 postgres:16
docker run -d --name qdrant -p 6333:6333 -p 6334:6334 qdrant/qdrant

# 2. 拉取依赖
cd kb-service
go mod tidy

# 3. 启动服务（MockEmbedder 模式，无需 Embedding 服务）
export DATABASE_URL="postgres://postgres:pass@localhost:5432/kbdb?sslmode=disable"
go run main.go
```

## 项目结构

```
kb-service/
├── main.go                       # 入口：路由注册、依赖注入
├── go.mod
├── Dockerfile
├── internal/
│   ├── model/
│   │   └── model.go              # 所有数据结构（严格按规范字段）
│   ├── store/
│   │   ├── store.go              # MetaStore / VecStore 接口定义
│   │   ├── postgres.go           # MetaStore 实现（PostgreSQL，自动建表）
│   │   └── qdrant.go             # VecStore 实现（Qdrant KNN 检索）
│   ├── storage/
│   │   ├── oss.go                # ObjectStorage 接口定义
│   │   └── local.go              # 本地文件系统实现（开发 / 单机部署）
│   ├── parser/
│   │   ├── parser.go             # 文档解析 + 切块（§8.4 策略，800字/100字重叠）
│   │   └── embedder.go           # Embedding 接口（HTTP / Mock）
│   ├── worker/
│   │   └── worker.go             # 异步索引 worker（goroutine 池，4并发）
│   ├── llm/
│   │   ├── agent.go              # tool_calling SDK 初始化（复用 tool_calling_go）
│   │   ├── query_refine.go       # 模糊自然语言 query 意图精化（LLM动态处理）
│   │   └── conflict.go           # LLM 动态逻辑冲突检测（tool_calling 结构化输出）
│   └── handler/
│       ├── collections.go        # 集合 CRUD
│       ├── documents.go          # 文档索引/查询/删除
│       ├── upload.go             # 文件直传上传（multipart/form-data）
│       ├── query.go              # 语义检索（含 LLM query 精化）
│       ├── ingest.go             # 搜索沉淀 + 纯解析
│       └── util.go               # 分页等工具
├── pkg/
│   └── util/
│       ├── id.go                 # NewID(prefix)：coll_ / doc_ / chunk_
│       └── response.go           # 统一 code/message/data 响应
└── test/
    ├── parser/
    │   └── parser_test.go        # parser 层黑盒测试
    ├── storage/
    │   └── storage_test.go       # storage 层黑盒测试
    ├── handler/
    │   └── handler_test.go       # handler 层黑盒测试（mock 依赖）
    └── testutil/
        └── mock.go               # Mock MetaStore / VecStore / OSS + HTTP 辅助
```

## 与其他模块联调

### 一、核心调用方与接口清单

#### 1. 来自 Voice Agent（周浩洋）的调用

| 接口路径 | 调用方式 | 交互说明 |
|---------|---------|---------|
| `POST /api/v1/kb/query`（异步） | 异步请求 | VAD 触发后异步调用，检索 chunks 注入 LLM 上下文，完成后回调 `POST /api/v1/voice/ppt_message` |
| `POST /api/v1/kb/query-chunks`（同步） | 同步请求 | 同步知识库分块查询，直接返回 `[]chunk` |

#### 2. 来自 PPT Agent（钟天贻）的调用

| 接口路径 | 调用方式 | 交互说明 |
|---------|---------|---------|
| `POST /api/v1/kb/query-chunks`（同步） | 同步请求 | 获取知识库内容用于 PPT 生成 |
| `POST /api/v1/kb/parse` | 同步请求 | 纯解析 PDF/PPT 获取结构化文本块，不入向量库 |

#### 3. 来自 Web Search 的调用

| 接口路径 | 调用方式 | 交互说明 |
|---------|---------|---------|
| `POST /api/v1/kb/ingest-from-search` | Fire-and-Forget | 将搜索结果写入知识库，完成知识库的增量更新，无需结果返回 |

### 二、交互链路梳理

#### 1. 「查询类」交互链路

- **Voice Agent → kb-service**：
  语音代理作为核心入口，同时支持**异步全量查询**（`/kb/query`）和**同步分块查询**（`/kb/query-chunks`）两种模式，适配不同业务场景的响应需求。
- **PPT Agent → kb-service**：
  PPT代理仅通过**同步分块查询**（`/kb/query-chunks`）获取知识库内容，用于 PPT 生成相关的内容检索。

#### 2. 「数据写入类」交互链路

- **Web Search → kb-service**：
  网络搜索服务通过 `/kb/ingest-from-search` 接口，将外部搜索到的有效数据同步到知识库，实现知识库的动态更新与扩充，为后续查询提供更丰富的数据源。

### 三、关键特性总结

1. **双模式查询支持**：kb-service 同时提供异步/同步两种查询接口，适配 Voice Agent、PPT Agent 等不同模块的性能与响应需求。
2. **闭环数据流转**：通过 Web Search 的写入接口，实现「外部数据→知识库→业务查询」的完整闭环，保障知识库的时效性与丰富度。
3. **多模块协同**：作为核心数据服务，kb-service 承接了 Voice Agent、PPT Agent、Web Search 三个核心模块的交互，是整个系统的知识库中枢。

### 四、接口行为说明

#### POST /api/v1/kb/query（异步 + 主动回调）

支持两种模式（严格符合 `API_DOCUMENTATION.md` §2.1）：

- **异步模式**（传 `session_id`）：立即返回 `{"accepted": true}`，后台执行检索，完成后回调 `POST /api/v1/voice/ppt_message`，`msg_type: "kb_result"`，`summary` 字段携带摘要。
- **同步模式**（不传 `session_id`）：立即返回 `{"summary": "..."}`。

> `top_k` 必填且 > 0，传 0 或不传返回 `40001` 参数错误。`score_threshold` 传 `0.0` 表示不设阈值（允许），传 `>1.0` 返回 `40001` 参数错误。

#### POST /api/v1/kb/query-chunks（同步阻塞返回）

关键词检索接口，供记忆模块 / PPT Agent 调用，同步返回 chunk 列表。
传 `user_id` → 检索用户个人知识库；不传 → 仅检索专业知识库。

#### POST /api/v1/kb/upload

响应严格符合 `API_DOCUMENTATION.md` §5.1：

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "file_id": "doc_xxx",
    "filename": "lecture.pdf",
    "file_type": "pdf",
    "file_size": 1234567,
    "storage_url": "http://localhost:9200/storage/...",
    "purpose": "reference"
  }
}
```

文件大小上限 500 MB，超过返回 `40001`。同一用户下相同文件名不允许重复上传（`409` 冲突）。

#### POST /api/v1/kb/ingest-from-search

响应包含 `ingested`、`skipped`、`failed`、`doc_ids` 四个字段。
全部文档入库失败时（`ingested==0 && failed>0`）返回 `50000` 内部错误。
URL 去重检查遇到数据库错误时立即返回 `50000`，不再静默继续。

## 错误码

| code | 含义 |
|------|------|
| 200 | 成功 |
| 40001 | 参数缺失或非法（必填字段为空、top_k<=0、score_threshold>1.0、文件超过 500 MB） |
| 40002 | items 为空 |
| 40400 | 资源不存在 |
| 40900 | 资源冲突（文件已存在、内容已索引、URL 已入库） |
| 50000 | 服务内部错误 |
| 50200 | 向量数据库 / Embedding 服务不可用 |
