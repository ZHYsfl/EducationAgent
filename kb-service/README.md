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
| POST | `/api/v1/kb/query` | 语义检索（核心 RAG） |
| POST | `/api/v1/kb/ingest-from-search` | 搜索结果沉淀入库 |
| POST | `/api/v1/kb/parse` | 纯文档解析（不入向量库） |
| GET  | `/health` | 健康检查 |

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PORT` | `9200` | 服务监听端口 |
| `PG_DSN` | `postgres://postgres:postgres@localhost:5432/kbdb?sslmode=disable` | PostgreSQL DSN |
| `QDRANT_URL` | `http://localhost:6333` | Qdrant 向量库地址 |
| `QDRANT_COLLECTION` | `kb_chunks` | Qdrant collection 名称 |
| `OSS_BASE_PATH` | `./data/storage` | 本地文件存储根目录 |
| `OSS_BASE_URL` | `http://localhost:9200/storage` | 文件访问 URL 前缀 |
| `EMBED_SERVICE_URL` | 空 | Embedding HTTP 服务地址；空时使用 MockEmbedder（非零占位向量，仅开发用）|
| `PYTHON_PARSER_URL` | 空 | Python 解析微服务地址；空时仅支持 text/web_snippet 直接切块 |
| `LLM_API_KEY` | 空 | LLM 密钥；空时禁用 query 意图精化，直接使用原始查询文本 |
| `LLM_MODEL` | `gpt-4o-mini` | LLM 模型名（兼容 OpenAI 接口）|
| `LLM_BASE_URL` | 空 | 自定义 LLM API 地址（如本地部署或第三方兼容接口）|

### embed-server（可选，推荐）环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `HF_TOKEN` | 空 | Hugging Face Token（必填） |
| `HF_MODEL` | `BAAI/bge-m3` | Hugging Face 模型名 |
| `EMBED_PORT` | `8000` | embed-server 监听端口 |

## 快速启动（开发模式）

```bash
# 1. 启动依赖
docker run -d --name postgres -e POSTGRES_PASSWORD=pass -e POSTGRES_DB=kbdb -p 5432:5432 postgres:16
docker run -d --name qdrant -p 6333:6333 -p 6334:6334 qdrant/qdrant

# 2. 拉取依赖
cd kb-service
go mod tidy

# 3. 启动服务（MockEmbedder 模式，无需 Embedding 服务）
export PG_DSN="postgres://postgres:pass@localhost:5432/kbdb?sslmode=disable"
go run main.go
```

## 项目结构

```
kb-service/
├── cmd/
│   └── embed-server/
│       └── main.go               # Go 版 Embedding 服务（转发到 HF bge-m3）
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

### Voice Agent（周浩洋）→ KB Service

1. **RAG 语义检索**：VAD 触发后异步调用 `POST /api/v1/kb/query`，将检索到的 chunks 注入 LLM 上下文。
   - 需传 `user_id`（必填）、`query`（当前 ASR 部分文本）
   - `collection_id` 可选，为空则搜索该用户全部集合

2. **CRAG 知识沉淀**：当 KB 检索分数低（`score < score_threshold`）且 Web Search 模块返回有价值结果时，**Voice Agent 自行判断**并异步调用 `POST /api/v1/kb/ingest-from-search`。
   - **注意**：KB Service 本身不主动触发 Web Search，也不感知检索评分是否达标；"是否触发沉淀"的判断逻辑完全在 Voice Agent 侧。
   - KB Service 只负责接收 items、URL 去重、异步入库。

3. **文档索引触发**：用户通过前端上传文件后，可直接调用 `POST /api/v1/kb/upload`（multipart 文件直传），或由 Voice Agent 拿到外部 `file_url` 后调用 `POST /api/v1/kb/documents` 触发索引。

### PPT Agent（钟天贻）→ KB Service

- 调用 `POST /api/v1/kb/parse` 解析用户上传的参考资料（PDF/PPT 等），获取结构化文本块供课件生成使用。
- **不入向量库**，纯解析返回，不影响 RAG 索引。
- 若 `PYTHON_PARSER_URL` 未配置，非 text 类型文件返回占位结果，需联调时确保 Python 解析服务已启动。

### Database Service（曾晨曦）→ KB Service

- KB Service **不直接调用** Database Service 接口。
- 依赖关系是单向的：Database Service 存文件 → 上层（Voice Agent）拿到 URL → 调用 KB Service 索引。
- KB Service 存储的 `file_id` 字段仅做关联记录，不做反向查询。

## 错误码

| code | 含义 |
|------|------|
| 200 | 成功 |
| 40001 | 参数缺失或非法（必填字段为空）|
| 40002 | items 为空 |
| 40400 | 资源不存在 |
| 50000 | 服务内部错误 |
| 50200 | 向量数据库 / Embedding 服务不可用 |
