# database_service_go

独立 **Database Service**（§0.7），**默认端口 `9500`**。

- **对外**：`/api/v1/sessions` + `/api/v1/tasks` + `/api/v1/files`（上传/查询/删除）
- **对内**：`/internal/db/*` → 供 PPT Agent `HTTPPPTRepository` 持久化 tasks / exports

持久化默认 **PostgreSQL**；可选回退 **Redis**（键前缀 `ppt_db:`）。

## 构建与运行

```bash
cd database_service_go
go mod tidy
set PPT_DB_REDIS_URL=redis://127.0.0.1:6379/1
set INTERNAL_KEY=your-shared-secret
go run ./cmd/database-service/
```

与 PPT Agent 画布共用 Redis 时，建议业务库用 **独立 logical DB**（如 `/1`），画布用 `/0`。

### PostgreSQL（默认）

```bash
set PPT_DATABASE_BACKEND=postgres
set PPT_DB_PG_DSN=postgres://user:pass@127.0.0.1:5432/educationagent?sslmode=disable
set PPT_DATABASE_ENABLED=true
go run ./cmd/database-service/
```

## 环境变量

| 变量 | 说明 | 默认 |
|------|------|------|
| `DATABASE_SERVICE_PORT` | 监听端口 | `9500` |
| `DATABASE_SERVICE_HOST` | 监听地址 | `0.0.0.0` |
| `PPT_DATABASE_BACKEND` | `postgres` 或 `redis` | `postgres` |
| `PPT_DB_PG_DSN` / `DATABASE_URL` | PostgreSQL 连接串 | `postgres://postgres:postgres@127.0.0.1:5432/educationagent?sslmode=disable` |
| `PPT_DB_REDIS_URL` | Redis URL；空则用 `REDIS_URL` | 空 |
| `PPT_DB_REDIS_PREFIX` | 键前缀 | `ppt_db:` |
| `PPT_DATABASE_ENABLED` | 是否启用持久化 | `true` |
| `INTERNAL_KEY` | PPT → `/internal/db/*` 的 `X-Internal-Key` | 空 |
| `PPT_JWT_SECRET` | 对外 sessions 的 JWT | 空 |
| `PPT_FILE_UPLOAD_DIR` | 文件上传本地落盘根目录 | `uploads` |

## 与 PPT Agent 联调

1. 先启动本服务（9500），再启动 PPT Agent（9100）。
2. PPT Agent 设置：`PPT_DATABASE_SERVICE_URL=http://127.0.0.1:9500`。
3. 与 PPT 使用相同 `INTERNAL_KEY`（生产必配）。

模块路径：`educationagent/database_service_go`。
