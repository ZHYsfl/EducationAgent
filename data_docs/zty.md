# `zh-ppt-voice-agent-interrupt-dialogues.jsonl` 与各 `zty*` 源文件

**聚合文件**：`dataset/zh-ppt-voice-agent-interrupt-dialogues.jsonl`（**6235** 行，去重 **6173** 行）。命中口径：源文件每一非空行与聚合文件整行完全一致则计 1 条。「在 `zh-ppt` 中行号」为上述聚合文件中的**物理行号**（含首尾，各源文件在聚合内为连续区间）；未收录记为 —。

| 源文件 | 行数 | 聚合命中 | 在 `zh-ppt` 中行号 | 指南类别（简） |
|--------|------|----------|-------------------|----------------|
| `dataset/zty.jsonl` | 900 | 900 | 3981–4880 | Phase 1 有 action，未打断 |
| `dataset/zty_1-2.jsonl` | 225 | 225 | 5561–5785 | 纯对话无 action，未打断（Phase 1 system） |
| `dataset/zty_1-2_copy.jsonl` | 225 | 225 | 5336–5560 | 纯对话无 action，未打断（Phase 2 system） |
| `dataset/zty_1-3.jsonl` | 225 | 225 | 6011–6235 | 纯对话无 action，打断（Phase 1 system） |
| `dataset/zty_1-3_copy.jsonl` | 225 | 225 | 5786–6010 | 纯对话无 action，打断（Phase 2 system） |
| `dataset/zty_2-E.jsonl` | 300 | 0 | — | Phase 2的E类型，仅 `send`（`status` 均为 empty） |
| `dataset/zty_2-mixed.jsonl` | 150 | 0 | — | Phase 2：队列混合，fetch+send与仅send混合 |
| `dataset/zty_2-not empty.jsonl` | 150 | 0 | — | Phase 2：队列非空 + fetch+send |

（2026-05-14）
