# ppt_agent_service_go

与 Python `ppt_agent_service` 对齐的 PPT Agent HTTP 服务（Go）。全册生成走 **`toolcalling`**（`PPTAGENT_*` 环境变量），幻灯片 HTML 由 `internal/deckgen` 生成。

## 运行

```bash
set PPT_DATABASE_SERVICE_URL=http://127.0.0.1:9500
set PPTAGENT_MODEL=...
set PPTAGENT_API_BASE=...
set PPTAGENT_API_KEY=...
set REDIS_URL=redis://127.0.0.1:6379/0
go run ./cmd/ppt-agent
```

其中 `PPT_DATABASE_SERVICE_URL` 指向 `database_service_go`。该服务现默认使用 PostgreSQL 持久化；`REDIS_URL` 仅用于本服务的缓存/快照相关能力，不作为主数据库。

可选：`PPT_AGENT_HOST`、`PPT_AGENT_PORT`、`PPT_RUNS_DIR`、`PPT_PUBLIC_BASE_URL`、`PPT_JWT_SECRET` / `INTERNAL_KEY`、`PPT_AUTH_DISABLED=true`。

与 Python 一致的可选校验与语音：

- **`PPT_USER_VERIFY=allowlist`** + **`PPT_USER_ALLOWLIST`**（逗号分隔）：`ppt/init` 与 `POST /tasks` 对 `user_id` 做白名单校验（否则 404 文案 `user_id 不存在`）。
- **`PPT_VOICE_MESSAGE_URL`**：优先使用；PPT Agent 反向调用 Voice Agent 的 `POST /api/v1/voice/ppt_message`（§3.4）。
- **`VOICE_AGENT_BASE_URL`**：可选；配置后会自动尝试 `${base}/api/v1/voice/ppt_message`，失败时回退 `${base}/api/v1/voice/ppt_message_tool`（兼容别名）。
- **`PPT_VOICE_WEBHOOK_URL`**：旧配置兼容；未配置新项时继续使用该 URL。
- **`PPTAGENT_TEMPLATE`**：与 Python 一致，写入全册生成 **`ExtraContext`**（`【PPT模板】...`），供 LLM 选用版式风格（Go 侧不加载 `.pptx` 模板文件）。
- **`PPTAGENT_PRES_URL`**：可选；配置后 `RunDeck` 将优先调用该 URL（通常指向 `PPTAgent_go` 的 `/api/v1/generate-pres`）生成真实 PPTX 与页 HTML；未配置时回退本地 `deckgen`。
- **`KB_QUERY_URL`**：可选；如配置，`RunDeck` 会在生成前请求知识库检索（`POST` JSON），将结果注入 `extra_context` 与 `context_injections`。
- **`WEB_SEARCH_QUERY_URL`**：可选；如配置，`RunDeck` 会在生成前请求 Web Search（`POST` JSON），并将摘要注入生成上下文。
- **`KB_PARSE_URL`** / **`KB_SERVICE_URL`**：可选；用于统一参考资料解析接口。PPT Agent 暴露 `POST /api/v1/kb/parse` 并转调外部 KB 解析，再按标准 chunk 配置做二次归一化。
- **RAG/记忆/搜索注入**：`ppt/init` / `tasks` 支持可选字段 `extra_context`、`retrieval_trace`、`context_injections`，并在生成链路透传到 `PPTAgent_go`（或注入 deckgen 的 `extra_context`），用于跨模块上下文追踪与复现。
  - 若以上字段未由 Voice Agent 提供，则 `RunDeck` 可基于 `KB_QUERY_URL` / `WEB_SEARCH_QUERY_URL` 自动拉取并填充（best-effort，不阻断生成）。
- **`coder_plan` 结构化执行追踪**：当走 `PPTAGENT_PRES_URL`（`/api/v1/generate-pres`）时，响应中的 `coder_plan` 会透传执行器字段：`executed_actions`、`partial_actions`、`failed_index`、`failed_command`，供上层直接展示/回放/重试，无需再解析错误字符串。
- **参考资料统一解析**：`RunDeck` 会对 `reference_files`（pdf/docx/pptx/image/video/text）调用解析接口，按 `chunk_size=800`、`overlap=100`、`split_by=paragraph`、`min_chunk_size=100` 标准化后，将样本片段注入 `extra_context`。
- **画布渲染**：**`PPT_CANVAS_RENDER`** — `placeholder`（默认，占位 JPEG）、**`chromedp`**（Headless Chrome 截图，需本机 Chrome/Chromium）、**`auto`**（优先 chromedp，失败回退占位图）。可选 **`PPT_CHROME_PATH`** 指定浏览器可执行文件路径。
- **JSON 体解析失败**：返回 **`40001`**，**`message`** 前缀为 **`请求参数无效：`**，总长度截断约 800 字符（对齐 FastAPI `RequestValidationError` 处理风格）。

## 与 Python 的差异（简要）

- **HTTP 行为**：`ppt/init` 已拼接 **`build_effective_description`**（参考资料 + 教学要素兜底）；**`feedback`** 已按 **`validate_feedback_intents`** 全量校验（含 `global_modify`→`ALL`、`resolve_conflict`→`reply_to_context_id`、完成态 `page_id`、instruction/delete 默认文案、**二次校验**子请求与 **`lines_merge` 入队**）；**`PUT .../status`** 错误信息列出允许状态；**`VAD`** timestamp 错误文案与 Python 一致；**进程退出**时取消悬挂页 watcher（对齐 lifespan）；**未匹配路由**返回 **`40400` / `资源不存在`**（对齐 Starlette 404）。
- **合并引擎**：同 Python **`merge_engine` / `rule_merge_apply` / `feedback_pipeline`**（见上文环境变量说明）。
- **导出**：支持 **`pptx/docx/md/html5`**；其中 `docx` 为 Go 侧原生导出。成功导出后触发 **`ppt_status` 语音/Webhook**。
- **渲染**：默认 **占位 JPEG**；设 **`PPT_CANVAS_RENDER=chromedp`** 时与 Python **html2image+PIL** 类似，从 `py_code` 取 HTML 并输出 **`renders/slide_XXXX.jpg`**（Chrome 不可用时返回 **`50210`** `渲染依赖不可用：...`，与 Python 缺依赖一致）。**`auto`** 模式在后台路径失败时静默回退占位图。
- **全册 PPTX**：未配置 `PPTAGENT_PRES_URL` 时仍写**空 `output.pptx` 占位**；配置后可接入 `PPTAgent_go` 产出真实 `pptx_path/applied_pptx_path`。

## 模块

- `cmd/ppt-agent`：入口
- `internal/pptserver`：路由与 HTTP 处理
- `internal/gen`：`RunDeck` 全册生成
- `internal/toolllm`：`toolcalling` 封装
- `internal/dbclient`：Database Service 客户端
- `internal/exportmd`：教案 Markdown 导出
- `internal/merge`：三路合并与反馈流水线

`go.mod` 中 `replace toolcalling => ../tool_calling_go` 需与本仓库目录结构一致。
