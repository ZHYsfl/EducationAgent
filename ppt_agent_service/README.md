# PPT Agent HTTP 服务

符合《系统接口规范》第 0 / 3 章约定：**默认端口 9100**、**JWT 鉴权**、**统一业务错误码**、**Redis 画布**与 **VAD 快照**。

**数据库**：本进程 **不内嵌 SQL**，任务/会话/导出持久化 **仅通过独立 Database Service（§0.7，默认 :9500）**。

## 启动

```bash
# 1) 先启动 Database Service（另开终端）
pip install -r ppt_agent_service/requirements.txt
set PPT_DATABASE_URL=sqlite+aiosqlite:///./runs/ppt_agent.db
python -m database_service

# 2) 再启动 PPT Agent（需 Redis）
set PPT_DATABASE_SERVICE_URL=http://127.0.0.1:9500
set INTERNAL_KEY=your-shared-secret   # 与 Database Service 一致，模块间写库必填
python -m ppt_agent_service
# 或
uvicorn ppt_agent_service.app:app --host 0.0.0.0 --port 9100
```

未设置 **`PPT_DATABASE_SERVICE_URL`** 时，进程在 **启动阶段即报错退出**。

环境变量：

| 变量 | 说明 | 默认 |
|------|------|------|
| **`PPT_DATABASE_SERVICE_URL`** | **必填**。Database Service 根 URL，如 `http://127.0.0.1:9500` | 无（未配置则无法启动） |
| `INTERNAL_KEY` | **强烈建议配置**。PPT → Database Service 的 `X-Internal-Key`（§0.3）；与 JWT 二选一即可 | 空 |
| `PPT_DATABASE_SERVICE_TIMEOUT` | 转发 Sessions / HTTP 仓库请求超时（秒） | `120` |
| `PPT_AGENT_PORT` | 监听端口 | `9100` |
| `PPT_AGENT_HOST` | 监听地址 | `0.0.0.0` |
| `REDIS_URL` | Redis 连接串 | `redis://127.0.0.1:6379/0` |
| `PPT_SNAPSHOT_TTL_SEC` | VAD 快照 TTL（秒） | `300` |
| `PPT_PUBLIC_BASE_URL` | 公网基址（无前缀 `/`），用于把 `/static/runs/...` 等拼成绝对 URL | 空（保持相对路径） |
| `VOICE_AGENT_BASE_URL` | Voice 服务地址（`ppt_message`） | `http://localhost:9000` |
| `PPT_JWT_SECRET` | HS256 校验 **`Authorization: Bearer <token>`**；载荷需含 **`user_id`**（或 `sub`），建议含 **`exp`** | 空 |
| `PPT_JWT_ALGORITHM` | JWT 算法 | `HS256` |
| `PPT_JWT_REQUIRE_EXP` | 是否强制校验 `exp` | `true` |
| `PPT_AUTH_DISABLED` | `true` 时关闭鉴权（仅本地调试） | `false` |
| `PPT_393_APPLY_LLM_FALLBACK` | §3.9.3 规则合并未命中可执行规则时是否仍调用编辑 LLM | `true` |
| `PPT_MERGE_USE_LLM` | 是否在「代码冲突」路径调用合并裁决 LLM | `true` |
| `PPT_SUSPEND_RELATED_USE_LLM` | §3.9.1 用 LLM 判相关/无关（关则始终视为无关并重播） | `true` |
| `PPT_SUSPEND_RELATED_MODEL` | 可选，悬挂相关判定专用模型（更小更快）；空则用 `PPTAGENT_MODEL` | 空 |

> **`PPT_DATABASE_URL` / `PPT_DATABASE_ENABLED` / `PPT_DATABASE_ECHO`** 仅由 **`database_service`** 使用，**不要**指望在 PPT Agent 进程内连接数据库。

### §7.2 / §7.3 数据库与会话（独立 Database Service）

- **表与 ORM** 在仓库 **`ppt_agent_service/db/`**，由 **`python -m database_service`** 进程加载并 `init_db()`。
- **PPT Agent**：通过 **`HTTPPPTRepository`** 调用 Database Service 的 **`/internal/db/*`** 完成 `save_task` / `load_task` / exports 等；**`GET|POST|PUT /api/v1/sessions`** 仅 **HTTP 转发** 到 Database Service（路径与鉴权头原样传递）。
- **任务**：`POST /api/v1/ppt/init`、`POST /api/v1/tasks` 会经 HTTP **`ensure` users/sessions**；运行中 **`persist_task`** 写回 Database Service。
- **冷启动**：`GET /api/v1/tasks/{id}` 内存未命中时从 Database Service **加载完整 `TaskState`**。
- **PostgreSQL 生产**：`database_service` 侧设置 `PPT_DATABASE_URL=postgresql+asyncpg://...` 并安装 **`asyncpg`**。

### §0.3 鉴权说明

- 除 **`/healthz`**、**`/docs`**、**`/redoc`**、**`/openapi.json`**、**`/favicon.ico`** 及 **非 `/api/v1/*`** 路径外，所有 **`/api/v1/*`** 请求须满足其一：
  - **`Authorization: Bearer <jwt>`**（密钥为 `PPT_JWT_SECRET`），或
  - **`X-Internal-Key: <INTERNAL_KEY>`**（与 Voice 等后端互通）。
- 未配置 `PPT_JWT_SECRET` 且未配置 `INTERNAL_KEY` 时，**不启用鉴权**（兼容旧部署）。
- JWT 场景下：**`ppt/init`、`POST /api/v1/tasks` 的 `user_id` 必须与 token 中 `user_id`（或 `sub`）一致**；带 `task_id` 的接口会校验任务归属（内部密钥可访问任意任务）。
- **`GET /api/v1/tasks`**：JWT 用户仅能列出 **同 `session_id` 且 `user_id` 与本人一致** 的任务。

### §0.2 错误码（响应 HTTP 仍为 200，读 JSON `code`）

| `code` | 说明 |
|--------|------|
| `200` | 成功 |
| `40001` | 参数错误（含 Pydantic 校验失败） |
| `40100`–`40104` | 鉴权：缺失、过期、无效、user 不一致、无权访问任务 |
| `40400` | 资源不存在 |
| `40900` | 状态冲突 |
| `50000` | 服务内部错误（未捕获异常统一归此码） |
| `50200` | 依赖不可用（如 Redis 不可用、VAD 写快照失败、Database Service 不可达） |
| `50210` | 渲染/模型类依赖不可用（如 `canvas/render/execute` 缺 html2image；合并 LLM 失败仅记日志并降级启发式） |

## `py_code`（§3.1 / §3.7）

每页 `py_code` 为 **合法 Python 源码**：定义 `get_slide_markup() -> str`，`return` 后为 `repr(HTML)`，便于规范中的渲染执行器 `exec`/合并。历史任务若仍为裸 HTML，导出接口会自动按原样拼接。

## 导出（§3.6）

- **`POST /api/v1/ppt/export`**，`format`：`pptx`（拷贝生成结果）、**`docx`（教案 Word）**、`html5`。
- **`docx`**：由 **`lesson_plan_docx.build_lesson_plan_docx`** 生成，含课程标题、基本信息、结构化 **`teaching_elements`**、**`description`** 摘要，以及按页从幻灯片 HTML 抽取的正文要点（依赖 **`python-docx`**）。

## Redis 与 VAD

- **当前画布**：`canvas:{task_id}`，JSON：`page_order`、`current_viewing_page_id`、`timestamp`；**`pages[page_id]` 仅含 §3.1.2 PageCode 三字段**（`page_id` / `py_code` / `status`）；**`page_display[page_id]`** 含 `render_url`、`last_update`。
- **VAD 快照**：`POST /api/v1/ppt/vad` 与 **`POST /api/v1/canvas/vad-event`**（同逻辑，后者与部分文档路径一致），Body 为 `task_id`、`timestamp`（Unix ms）、`viewing_page_id`。写入 **`snapshot:{task_id}:{timestamp}`**，TTL 300s（可配置）；快照内 **`pages` 仅保留上述三字段**（无 `page_display`），并带 `vad_viewing_page_id` 便于与 `ppt/feedback` 的 `base_timestamp` 对齐。成功时 `data` 仅含 **`accepted: true`**（不返回 Redis 快照 key）。
- **联调**：Voice/网关应在 **vad_start** 时调用上述任一接口；若改为直连 Redis，需与本服务的文档形状保持一致。
- **`POST /api/v1/ppt/feedback`**：若 `viewing_page_id` 为有效页 ID，会同步 **`current_viewing_page_id`** 并写回 Redis 画布。

若 Redis 未就绪：`GET /api/v1/canvas/status` 仍可从内存任务回退；`POST /api/v1/ppt/vad`（或 `/api/v1/canvas/vad-event`）返回业务码 **50200**。

## ID（§0.4）

- `task_id` / `page_id` / `export_id` / `context_id`：`task_{uuid4}`、`page_{uuid4}`、`file_{uuid4}`、`ctx_{uuid4}`（标准 UUID v4 字符串，含横杠）。

## 三路合并、悬挂页与 Voice 求助（§3.8–§3.9 / §3.4）

- **`POST /api/v1/ppt/feedback`**（任务 `completed`）：按页 / `global_modify` 桶做三路合并；用 Redis **`snapshot:{task_id}:{base_timestamp}`** 与当前 `py_code` 生成 **system_patch**（diff）。
- **§3.9.3 三条路径**（`merge_engine.decide_three_way_merge`）：
  - **代码不冲突**（无实质 diff / 无快照视为未漂移）**且逻辑不冲突**（无二选一、多问号等歧义）→ **`auto_resolved`，不调用合并 LLM**（`rule_merge_path`）；改页时先走 **`rule_merge_apply.try_rule_apply_html`**（标题/引号替换/简单颜色等），未命中时由 **`PPT_393_APPLY_LLM_FALLBACK`**（默认 `true`）决定是否再调 **编辑 LLM**；设 `false` 则严格保持 `V_current` 仅记 description。
  - **代码冲突 + 逻辑不冲突** → 调用 **合并 LLM**（或 `PPT_MERGE_USE_LLM=false` 时启发式）产出 `merged_pycode` / `ask_human`。
  - **代码冲突 + 逻辑冲突** → 合并 **LLM** 倾向 `ask_human`。
  - **代码不冲突 + 逻辑冲突** → 直接 **悬挂问人**（不调用合并 LLM）。
- 环境变量：`PPT_MERGE_USE_LLM`、`PPTAGENT_MODEL`、`PPTAGENT_API_KEY`、`PPT_393_APPLY_LLM_FALLBACK`。
- **`ask_human`**：页状态 **`suspended_for_human`**，生成 **`ctx_`…** 写入 `open_conflict_contexts`，并 **`POST`** Voice 的 **`ppt_message` + `ppt_message_tool`**，`msg_type=conflict_question`，`priority=high`。
- **§3.9.1**：悬挂 **45s** 无回应再次 high 播报；**180s** 自动解除并写入 `[系统自决-悬挂超时…]` 后触发生成。**仅当新反馈与悬挂问题「无关」**（由 **LLM 二分类**判定）时入队并 **再次 high 求助**；**相关**则**只入队、不重播**。未配模型、关闭开关或 LLM 失败/解析失败时 **视为无关**（会重播）。环境变量：`PPT_SUSPEND_RELATED_USE_LLM`、`PPT_SUSPEND_RELATED_MODEL`（可选，空则用 `PPTAGENT_MODEL`）。
- **§3.9.2 链式合并**：同 bucket 排队意图在上一轮完成后，**V_base = 上一轮合并后的各页 `py_code`**（`PageMergeState.chain_baseline_pages`），**V_current** 仍从 Redis 画布读取；**新的 `base_timestamp`（新一次 VAD）** 会清空该 bucket 的链式基线并改用新快照。排队的 intent **不再**用队列里保存的旧 `base_timestamp` 去拉快照。
- **`global_modify`**：`system_patch` 为按 **`page_order` 拼接的整册 V_base** 与 **同样顺序拼接的 V_current** 之间的 diff（`build_system_patch_global`），不再用单页 `page_for_patch` 对整册 current。
- **`resolve_conflict`** + **`reply_to_context_id`**：解除悬挂、合并排队说明进 `description`，再用 **同一套 AsyncLLM** 改该页 HTML 并 `schedule_partial_refresh`；**悬挂期间积累的 `pending_feedbacks` 会再走三路合并管线**（`start_merge_background`），而非仅追加文本。
- **§3.4**：调用 Voice `ppt_message` / `ppt_message_tool` 后解析 JSON，仅当 **`code == 200`** 或 **`message == "accepted"`**（忽略大小写）视为成功。
- **§3.8.3**：**`POST /api/v1/ppt/canvas/render/execute`**，Body：`task_id`、`page_id`、可选 `py_code`。成功时 `code=200` 且 `data` 含 `render_url`；缺依赖时为 **`50210`**，其它失败为 **`50000`**（`message` 含原因）。

## 首次生成 vs 后续修改

- **首次 / 整册重跑**：`PPTGenerator.generate_deck` → 完整 **PPTAgent.generate_pres**（模板 + 检索等不变）。
- **后续 `modify` / `global_modify` / 合并 auto / 冲突解除**：**不再调用 PPTAgent**；在 §3.9.3 规则路径下优先 **`rule_merge_apply`**，否则用 **`edit_slide_html_llm`** 改 HTML，写入 `py_code`，再 **`refresh_slide_assets`**（`html2image` 更新 `renders/slide_XXXX.jpg`，`python-pptx` 尽力同步 `output.pptx` 首文本框）。
- **增删页**（`insert_*` / `delete`）仍走 **整册重生成**（结构变化）。
- 环境变量：`PPT_SUSPEND_REASK_SEC`（默认 45）、`PPT_SUSPEND_AUTORESOLVE_SEC`（默认 180）、`VOICE_AGENT_BASE_URL`、`PPT_MERGE_USE_LLM`、`PPT_393_APPLY_LLM_FALLBACK`。
