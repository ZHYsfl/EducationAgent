# fullgen — 纯 Go 完整编排 + 方案 B（gopptx）

目标：在 **单 Go 进程** 内复现 Python `pptagent` 的 **多角色 YAML + Jinja 模板 -> LLM**，并用开源 **gopptx** 写 PPTX（不调用 Python 子进程）。

## 组件

| 包 | 职责 |
|----|------|
| `domain` | 模板根路径、`slide_induction.json` 解析（`language`、`functional_keys`、各 layout 块） |
| `agent` | 加载 `roles/*.yaml`（与 Python 同文件），**gonja** 渲染 `template`，调用 `pkg/infer` |
| `pptx` | **gopptx** 打开/保存 `source.pptx`，**往返**与后续形状/文本/图片操作 |
| `orchestrator` | 组装 `TemplateBundle` + `RoleRunner`；后续在此实现 `generate_outline` / `generate_slide` 与 Python 同序调用 |

## gopptx / 版本要求

- 依赖：`github.com/kenny-not-dead/gopptx`（BSD-3-Clause，开源）
- 该模块当前要求 **Go >= 1.24**，因此 `PPTAgent_go/go.mod` 已升到 `go 1.24`。

## 「像素级 / 字节级」PPTX 与 Python 一致

- **OpenXML 等价**：同一模板经 Go `Open` -> `SaveAs` 后，ZIP 条目顺序与属性顺序可能与 **python-pptx** 不同，文件哈希不一定相同，但可做到视觉一致。
- **推荐验收**：
  1. **往返基线**：`pptx.RoundTrip` 后对比 `source.pptx` 与输出（先比解压后各 `slide*.xml` 的规范化 XML）。
  2. **端到端**：同一 `Document` + outline，Python 与 Go 各导出 PPTX，逐页渲染 PNG 做 SSIM/像素 diff。
  3. CI 固定模板版本与字体，避免环境漂移。

## 环境变量（建议）

- `PPTAGENT_TEMPLATES_ROOT`：指向 `PPTAgent/pptagent/templates` 的绝对路径（内含 `default/source.pptx`）。
- `PPTAGENT_ROLES_ROOT`：指向 `PPTAgent/pptagent/roles`（默认可与模板并列推导）。
- 推理仍使用 `PPTAGENT_MODEL` / `PPTAGENT_API_BASE` / `PPTAGENT_API_KEY`（`pkg/infer`）。

## 后续工作（编排完整性）

当前仓库已落地骨架：模板元数据解析、与 Python 同源的 YAML+Jinja 渲染+LLM、PPTX 往返。仍需按 `pptgen.py` 顺序移植：

1. `planner` -> outline JSON（Go 里用结构校验替代 Pydantic 动态模型）。
2. `_add_functional_layouts` 纯逻辑。
3. 每页 `generate_slide`：`layout_selector` -> `editor` / `coder` 链；占位符写入 gopptx 对应 shape/text/image。
4. 并发：`errgroup` + semaphore 对齐 `max_at_once`。
