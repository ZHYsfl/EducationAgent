# PPTAgent_go

移植 **推理** 与 **全册内容生成（大纲 + 逐页 HTML）**：

| Python（原） | Go（本目录） |
|--------------|----------------|
| `await llm(prompt, system_message=..., return_json=False)` | `POST /api/v1/infer`，body 含 `prompt` / `system_message` / `return_json` |
| 全册 `generate_deck`（无 `pptagent` 时） | `POST /api/v1/generate-deck`（见下文） |
| 全册真实 `generate_pres`（纯 Go） | `POST /api/v1/generate-pres`（服务内编排，不依赖 Python bridge） |
| 底层 Chat Completions | **`/api/v1/infer`** 与 **`/v1/chat/completions`** 均经 **`tool_calling_go`**（`ChatCompletionSimple` / `ChatCompletionForwardJSON` + openai-go/v3） |

`ppt_agent_service` 通过 **`PPTAGENT_INFER_URL`** 调用本服务；可按场景选择 **`/api/v1/generate-deck`**（快速 HTML 全册）或 **`/api/v1/generate-pres`**（纯 Go 全册 + 输出 `output.pptx`）。

联调建议：数据库统一走 `database_service_go`（默认 PostgreSQL），`ppt_agent_service_go` 设置 `PPT_DATABASE_SERVICE_URL=http://127.0.0.1:9500`。

## 环境变量（与 Python 一致）

| 变量 | 说明 |
|------|------|
| `PPTAGENT_MODEL` | 默认语言/通用模型名 |
| `PPTAGENT_VISION_MODEL` | 可选；多模态请求且 `use_vision_model=true` 时使用；未设则与 `PPTAGENT_MODEL` 相同 |
| `PPTAGENT_API_BASE` | 兼容 OpenAI 的 API 根，如 `https://api.openai.com/v1` |
| `PPTAGENT_API_KEY` | Bearer Token |

可选：

| 变量 | 说明 |
|------|------|
| `PPTAGENT_SERVER_HOST` | 默认 `0.0.0.0` |
| `PPTAGENT_SERVER_PORT` | 默认 `9300` |
| `PPTAGENT_REPO_ROOT` | 仓库根目录（用于推导 `PPTAGENT_TEMPLATES_ROOT`） |
| `PPTAGENT_TEMPLATES_ROOT` | 模板根目录，形如 `.../PPTAgent/pptagent/templates` |
| `PPTAGENT_STAGE1_EMBED_URL` | 可选；Stage I live 模式下的 embedding 接口（返回向量后启用 cosine 聚类） |
| `PPTAGENT_STAGE1_CLUSTER_THRESHOLD` | 可选；Stage I 聚类阈值，默认 `0.86` |
| `PPTAGENT_CODER_MAX_RETRY` | 可选；coder 执行重试次数，默认 `3` |

## 作为库使用

```go
import "educationagent/pptagentgo/pkg/infer"

c, err := infer.NewClientFromEnv()
text, err := c.CompleteSimple(ctx, "你好", "你是助手")
```

## HTTP 服务（供系统调用）

```bash
cd PPTAgent_go
go mod tidy
set PPTAGENT_MODEL=...
set PPTAGENT_API_BASE=https://api.openai.com/v1
set PPTAGENT_API_KEY=sk-...
go run ./cmd/server/
```

### `POST /api/v1/infer`

简化接口（对齐 `prompt` + `system_message` + `return_json`）：

```json
{
  "prompt": "用户内容",
  "system_message": "系统提示",
  "return_json": false,
  "temperature": 0.7,
  "max_tokens": 4096,
  "image_urls": ["https://example.com/a.png", "data:image/jpeg;base64,..."],
  "image_detail": "auto",
  "model": "可选，覆盖环境变量模型",
  "use_vision_model": false
}
```

响应（统一包装）：

```json
{
  "code": 200,
  "message": "success",
  "data": { "ok": true, "content": "模型输出" }
}
```

### `POST /api/v1/generate-deck`

全册生成：先 JSON 大纲，再并发生成每页 HTML。请求体字段与 `ppt_agent_service` 对齐，常用项：

| 字段 | 说明 |
|------|------|
| `topic` | 课程主题 |
| `description` | 需求描述（含 `【字段】` 亦可） |
| `total_pages` | 页数；`<=0` 时服务内按 8 页 |
| `audience` / `global_style` | 受众与风格 |
| `teaching_elements` | 可选 JSON |
| `extra_context` | 可选长文本（Python 会注入 KB/检索/参考摘要） |
| `user_id` / `session_id` | 可选，便于日志与扩展 |

响应：`{ "ok": true, "slide_html": ["...", ...], "slide_count": N }`。

### `POST /api/v1/generate-pres`

纯 Go 路径：服务内生成 `slide_html`，并基于模板生成真实 `output.pptx`，返回 `pptx_path` 与页 HTML。  
当前已补齐 `planner -> outline -> functional layout -> layout_selector -> editor(content plan) -> coder(command draft)`（纯 Go，`/generate-pres` 响应含 `outline` / `layouts` / `content_plan` / `coder_plan` / `artifacts_dir`）。并新增：
- `generated_pptx_path`：把 `content_plan` 写成一份可读内容化 PPT（过渡写入器）
- `applied_pptx_path`：在模板 roundtrip 输出上按 element 名称语义写回文本 shape（优先返回 `applied_semantic.pptx`，模板保真更好）
- `image_plan`：图片语义映射计划（每页图片候选值 + 模板候选图片槽位），并落盘 `artifacts/image_plan.json`，用于后续接入真实图片替换器
- `image_apply_report`：图片替换执行报告（成功/跳过计数与原因）；执行产物为 `applied_semantic_with_images.pptx`（若有成功替换则优先作为 `applied_pptx_path` 返回）

版式精准写入（按 element 名称与模板语义逐项映射）仍在继续对齐中。

请求体（常用字段）：

```json
{
  "repo_root": "C:/Users/xxx/Desktop/AI/EducationAgent",
  "task_dir": "C:/tmp/ppt_task_001",
  "topic": "线性代数",
  "description": "【课程主题】...【教学目标】...",
  "extra_context": "由 Voice Agent 预先注入的检索摘要",
  "retrieval_trace": { "kb": {"request_id":"..."}, "memory": {"request_id":"..."} },
  "context_injections": [{ "source":"knowledge_base", "msg_type":"rag_chunks", "content":"..." }],
  "stage1_mode": "cached",
  "total_pages": 12,
  "template_name": "default"
}
```

`stage1_mode` 说明：

- `cached`（默认）：使用模板目录已有 `slide_induction.json`（与 Python 预分析缓存模式一致）。
- `live`：推理阶段对 `source.pptx` 运行 Stage I（启发式）生成 induction，并在本次请求内覆盖使用（用于无缓存或动态模板场景）。
  - 响应 `data.stage1_result` 与 `artifacts/stage1_result.json` 会包含分类簇、布局簇与本次生成的 induction，便于与 Python Stage I 对比验收。
  - 响应 `data.parity_report` 与 `artifacts/parity_report.json` 会输出推理同构验收摘要（Stage1/Stage2 完整性、coder 成功率、失败页），并包含：
    - `stage1_metrics.category_coverage_ratio`
    - `stage1_metrics.layout_cluster_count`
    - `stage1_metrics.schema_completeness`
    - `executor_metrics.history_coverage`
    - `executor_metrics.api_call_error_ratio`
    - `executor_metrics.comment_noise_ratio`

响应（统一包装，`data` 内与 `generate-deck` 同结构）：

```json
{
  "code": 200,
  "message": "success",
  "data": {
    "ok": true,
    "slide_html": ["<div>...</div>"],
    "slide_count": 12,
    "coder_plan": [
      {
        "slide_index": 1,
        "layout": "title_and_body",
        "executed_actions": ["replace_span(1)", "clone_paragraph(4)->9"],
        "html": "<div>...</div>"
      },
      {
        "slide_index": 2,
        "layout": "image_left_text_right",
        "executed_actions": ["replace_image(6)"],
        "partial_actions": ["replace_span(2)"],
        "failed_index": 2,
        "failed_command": "replace_image(999, \"xx.png\")",
        "error": "coder validation/retry failed: ...",
        "html": "<div>...</div>"
      }
    ]
  }
}
```

`coder_plan` 的执行追踪字段说明：

- `executed_actions`：本页最终成功执行的动作日志（按执行顺序）。
- `partial_actions`：重试耗尽时，失败前已经执行成功的动作（便于增量回放）。
- `failed_index`：失败命令序号（从 1 开始）。
- `failed_command`：失败命令片段（已做长度截断）。

### `POST /v1/chat/completions`

标准 OpenAI 请求体（`model` 可省略，由环境变量 `PPTAGENT_MODEL` 填充），便于现有 OpenAI SDK 把 Base URL 指到本服务。

## 纯 Go 完整编排 + PPTX（方案 B）

目录 **`internal/fullgen`**：在 **单 Go 进程** 内加载与 Python 相同的 **`PPTAgent/pptagent/roles/*.yaml`**，用 **gonja** 执行 Jinja 模板后调用 **`pkg/infer`**；用开源 **gopptx**（`github.com/kenny-not-dead/gopptx`）读写 **`source.pptx`**（含 **RoundTrip** 基线）。

- 说明与 **gopptx 的 Go 版本要求（>=1.24）**、**字节级与 python-pptx 差异**、后续待移植的 `generate_pres` 步骤：见 **`internal/fullgen/README.md`**。
- 环境变量：**`PPTAGENT_TEMPLATES_ROOT`**（指向 `pptagent/templates`）、**`PPTAGENT_ROLES_ROOT`**（指向 `pptagent/roles`，可省略则由 templates 的父目录推导）。

## 模块路径

`go.mod`: `module educationagent/pptagentgo`

在 monorepo 外使用时请配置 `replace` 或发布模块。
