# Database Service（§0.7）

独立进程，**默认端口 `9500`**，与《系统接口规范》中 Database Service 端口一致。

## 职责

- **对外**：`§7.3.7`–`§7.3.10` → `POST/GET/PUT /api/v1/sessions`（与 PPT Agent 内聚模式下同形）
- **对内**：` /internal/db/* ` → 供 PPT Agent 在配置 `PPT_DATABASE_SERVICE_URL` 时通过 `HTTPPPTRepository` 持久化 **tasks / exports / ensure user-session**

## 启动

```bash
# 与 PPT Agent 共用依赖
pip install -r ppt_agent_service/requirements.txt

# 需与 PPT 使用同一套 DB 配置，例如：
# set PPT_DATABASE_URL=sqlite+aiosqlite:///C:/path/to/ppt_agent.db
# set PPT_DATABASE_ENABLED=true
python -m database_service
```

环境变量：

| 变量 | 说明 | 默认 |
|------|------|------|
| `DATABASE_SERVICE_PORT` | 监听端口 | `9500` |
| `DATABASE_SERVICE_HOST` | 监听地址 | `0.0.0.0` |
| `PPT_DATABASE_URL` / `PPT_DATABASE_ENABLED` | 与 `ppt_agent_service.db.engine` 相同 | 见 PPT README |
| `INTERNAL_KEY` | PPT Agent 调用 `/internal/db/*` 时须携带 `X-Internal-Key`（与 §0.3 一致） | 空 |
| `PPT_JWT_SECRET` | 对外 `/api/v1/sessions` 的 JWT 校验（与 PPT 一致） | 空 |

## 与 PPT Agent 联调

1. **必须先启动本服务（9500）**，再启动 PPT Agent（9100）。
2. PPT Agent **必须**设置：`PPT_DATABASE_SERVICE_URL=http://127.0.0.1:9500`（未设置则 PPT 进程启动失败）。
3. 与 PPT 配置 **相同的 `INTERNAL_KEY`**（生产必配），以便 PPT 调用 `/internal/db/*` 时通过鉴权。

**注意**：**SQLite 仅适合本进程单写**；若与其它服务共库，请改用 **PostgreSQL** 等。
