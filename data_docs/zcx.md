# zcx.jsonl & zcx_phase2.jsonl & zcx_copy.jsonl 数据占比分析报告

> 分析日期：2026-05-15

---

# 第一部分：zcx.jsonl（Phase 1 数据）

> 数据文件：`zcx.jsonl`
> 数据总量：756 条（每条为一条完整多轮对话）

## 一、数据总览

| 类型 | 数量 | 占比 |
|------|------|------|
| **有 action（Phase 1）** | **756 条** | **100%** |

所有记录均为 Phase 1 阶段数据，每条记录包含完整的多轮对话序列。

---

## 二、对话打断分布

| 类型 | 数量 | 占比 |
|------|------|------|
| **有打断** | 756 条 | 100% |

所有数据均为打断场景，打断发生在第 1 轮 assistant 说话时。

---

## 三、消息结构

| Role | 数量 | 说明 |
|------|------|------|
| `system` | 756 | 每条记录包含 1 个 system 消息 |
| `user` | 756 | 每条记录包含 2 个 user 消息（第1个无打断，第2个有打断） |
| `assistant` | 1512 | 每条记录包含 2 个 assistant 消息 |
| `tool` | 756 | 每条记录包含 1 个 tool 消息 |

典型对话结构：
1. user（无打断，第1轮）
2. assistant（TTS 被截断）
3. user（`</interrupted>` 开头，第2轮）
4. assistant（正常输出 TTS + action）
5. tool（action 执行结果）

---

## 四、Action 类型统计

| Action | 数量 | 占比 |
|--------|------|------|
| `update_requirements` | 756 | 100% |
| `require_confirm` | 756 | 100% |
| `fetch_from_ppt_message_queue` | 0 | 0% |
| `send_to_ppt_agent` | 0 | 0% |

---

## 五、User 消息 status 分布

| status | 数量 |
|--------|------|
| `empty` | 756 |

Phase 1 期间所有 user 消息的 status 均为 `empty`。

---

# 第二部分：zcx_phase2.jsonl（Phase 2 数据）

> 数据文件：`zcx_phase2.jsonl`
> 数据总量：775 条（每条为一条完整多轮对话）

## 六、数据总览

| 类型 | 数量 | 占比 |
|------|------|------|
| **有效记录** | **775 条** | **100%** |

---

## 七、对话打断分布

| 类型 | 数量 | 占比 |
|------|------|------|
| **有打断** | 775 条 | 100% |

所有数据均为打断场景，但 user 消息**不带 `</interrupted>` 标记**（这是与 zcx.jsonl 的显著区别）。

---

## 八、消息结构

| Role | 数量 | 说明 |
|------|------|------|
| `system` | 756 | 每条记录包含 1 个 system 消息 |
| `user` | 756 | 每条记录包含 1 个 user 消息 |
| `assistant` | 1511 | 每条记录包含 2 个 assistant 消息 |
| `tool` | 756 | 每条记录包含 1 个 tool 消息 |

典型对话结构：
1. user（无 `</interrupted>` 标记）
2. assistant（TTS 被截断，如 `"好的，我会把全部文字字体设为微软"`）
3. user（`</interrupted>` 开头）
4. assistant（正常输出 TTS + action）
5. tool（action 执行结果）

---

## 九、Action 类型统计

| Action | 数量 | 说明 |
|--------|------|------|
| `send_to_ppt_agent` | 758 | 共 775 条记录，含多次调用 |
| `fetch_from_ppt_message_queue` | 758 | 共 775 条记录，含多次调用 |

所有记录均包含 `send_to_ppt_agent` 和 `fetch_from_ppt_message_queue` action。

---

## 十、User 消息 status 分布

| status | 数量 |
|--------|------|
| `empty` | 756 |

所有 user 消息的 status 均为 `empty`。

---

# 第三部分：zcx_copy.jsonl（Phase 2 纯对话数据）

> 数据文件：`zcx_copy.jsonl`
> 数据总量：456 条（每条为一条完整多轮对话）

## 十一、数据总览

| 类型 | 数量 | 占比 |
|------|------|------|
| **有效记录** | **456 条** | **100%** |

所有记录均为 Phase 2 纯对话数据，**无 action 调用**。

---

## 十二、对话打断分布

| 类型 | 数量 | 占比 |
|------|------|------|
| **有打断** | 456 条 | 100% |

所有数据均为打断场景。

---

## 十三、消息结构

| Role | 数量 | 说明 |
|------|------|------|
| `system` | 455 | 每条记录包含 1 个 system 消息 |
| `user` | 455 | 每条记录包含 1 个 user 消息 |
| `assistant` | 910 | 每条记录包含 2 个 assistant 消息 |

典型对话结构：
1. user（无打断）
2. assistant（TTS 被截断）
3. user（`</interrupted>` 开头）
4. assistant（正常输出 TTS，无 action）

---

## 十四、对话内容特征

纯对话数据中，assistant 仅输出口语文本，不包含任何 `<action>` 标签。

典型场景：
- 用户闲聊（如询问天气、"做个 PPT"）
- 用户中途改变需求
- 用户打断并提出新话题
- 用户对 assistant 的建议做出回应

---

# 第四部分：数据文件汇总对比

| 源文件 | 记录数 | 阶段 | 打断标记 | Action |
|--------|--------|------|----------|--------|
| `zcx.jsonl` | 756 | Phase 1 | `</interrupted>` | `update_requirements`, `require_confirm` |
| `zcx_phase2.jsonl` | 775 | Phase 2 | 无 `</interrupted>` | `send_to_ppt_agent`, `fetch_from_ppt_message_queue` |
| `zcx_copy.jsonl` | 456 | Phase 2 | `</interrupted>` | 无 action |

---

# 第五部分：与 dyp 数据集的差异分析

| 特征 | dyp 数据集 | zcx 数据集 |
|------|-----------|-----------|
| Phase 1 无打断数据 | 有（纯对话） | 无 |
| Phase 1 有打断数据 | 有 | 有 |
| Phase 2 fetch+send 混合 | 有 | 有 |
| Phase 2 仅 fetch | 有 | 无 |
| Phase 2 纯对话 | 有 | 有 |
| Phase 2 有 status=not empty | 有 | 无 |
| Phase 2 无打断数据 | 有 | 无 |

---

# 第六部分：数据特点总结

1. **Phase 1 打断场景一致**：zcx.jsonl 与 dyp 的 Phase 1 有 action 数据结构相似，均为用户打断后继续收集需求。

2. **Phase 2 特殊处理**：zcx_phase2.jsonl 中 user 消息不带 `</interrupted>` 标记，但 assistant 消息被截断，这可能表示一种"静默打断"场景。

3. **纯对话数据丰富**：zcx_copy.jsonl 提供了 456 条无 action 的纯对话数据，可用于训练闲聊能力。

4. **status 单一**：所有数据中 user 消息的 status 均为 `empty`，没有 `not empty` 场景的数据。
