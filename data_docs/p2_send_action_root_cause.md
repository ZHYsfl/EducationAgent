# Phase 2 "只回复不修改" 根因分析报告

> 问题描述：用户对 PPT 提出修改请求（如"帮我把字体调大"），Agent 只回复"好的"但没有实际调用 `send_to_ppt_agent` 转发给 PPT Agent。
>
> 分析日期：2026-05-15
>
> 数据范围：四个数据贡献者（wsb / zcx / dyp / zty）的所有 Phase 2 源文件及聚合文件 `zh-ppt-voice-agent-interrupt-dialogues.jsonl`

---

## 一、问题定性与数据规模

### 1.1 "只回复不修改" 在训练数据中的规模

对 Phase 2 源文件中所有"修改类请求"（用户说"帮我改X / 把X调大 / 增加X" 等明确动作请求）进行逐条标注：

| 源文件 | 修改类请求总数 | 其中有 send 动作 | 其中 TTS-only（无 action） | TTS-only 率 |
|--------|-------------|--------------|--------------------------|------------|
| `wsb_phase2.jsonl` | 42 条 | 20 条（48%） | **2 条** | 5% |
| `zcx_phase2.jsonl` | 395 条 | 203 条（51%） | **192 条** | **49%** |
| `phase2_dyp.jsonl` | 177 条 | 36 条（20%） | **35 条** | 20% |
| **合计** | **614 条** | **259 条（42%）** | **229 条（37%）** | **37%** |

> **关键发现**：37% 的"帮我改X"类请求在源数据中就没有对应的 `send_to_ppt_agent` 动作，assistant 直接 TTS"好的"结束。这些样本**直接教会了模型"帮我改X → 只回复不转发"**。

### 1.2 聚合文件中 Phase 2 动作组合分布

聚合文件（6235 条）按 Phase 2 记录统计：

| 动作组合 | 记录数 | 占比 |
|---------|-------|------|
| `only_send`（只发 send） | 1,504 条 | 24.1% |
| `only_fetch`（只发 fetch） | 511 条 | 8.2% |
| `mixed`（fetch + send） | 867 条 | 13.9% |
| `neither`（纯 TTS） | 3,353 条 | 53.8% |

---

## 二、根因分析

### 根因一（P0）：`zcx_phase2.jsonl` 源数据中近半数修改请求缺少 send 动作

**这是最主要的问题**。`zcx_phase2.jsonl`（756 条）本应负责提供"用户反馈 → 转发给 PPT Agent"的 `only_send` 模式数据，但它内部有严重缺陷：

- 395 条"帮我改X"类请求中，有 **192 条（49%）assistant 只说 TTS，不发 send**
- 示例：
  - 用户：`"帮我把第三页的配色改成深蓝色"`
  - Agent：`"好的"`（TTS，无任何 action）

这类样本在聚合后仍然带着"无 action"标签，导致模型在训练时反复学到：当用户明确要求修改时，Agent 的正确反应是说"好的"然后结束。

**修复方向**：修正 `zcx_phase2.jsonl` 中 192 条错误样本，为所有"修改类请求"补上 `send_to_ppt_agent` 动作。

### 根因二（P0）：`zty_2-E.jsonl`（300 条纯 only_send）缺失 system prompt，0% 命中聚合文件

`zty_2-E.jsonl` 提供了 300 条**纯粹的**"用户反馈 → 直接 send"样本，是最理想的 `only_send` 训练数据。但该文件：

- 每条记录**没有 system prompt**
- 聚合去重的整行匹配机制无法将其合并入聚合文件
- 导致 **300 条高质量 only_send 数据 100% 丢失**

**修复方向**：为 `zty_2-E.jsonl` 补上 Phase 2 system prompt，重新聚合。

### 根因三（P1）：`wsb_phase2.jsonl` 将所有用户反馈导向 fetch 动作

`wsb_phase2.jsonl`（476 条）的设计场景是"用户查队列状态"，它对**所有**用户消息都倾向于 fetch 队列，即使消息内容是明确的修改请求：

- 539 条 status_check 类请求（合理 → fetch）
- 但还有 42 条"帮我改X"类请求，其中 20 条走了 fetch 路线，2 条走了纯 TTS
- 整体 **0 条 only_send** 记录

这导致模型学到：在 Phase 2 中，用户说话 → 先 fetch 队列。真正的修改请求被混入了"查状态"的行为模式中。

**修复方向**：`wsb_phase2.jsonl` 应增加至少 100 条"帮我改X → send"的明确模式，补充 `only_send` 覆盖。

### 根因四（P1）：`phase2_dyp.jsonl` 的 send 覆盖率仅 20%

`phase2_dyp.jsonl`（450 条）虽然提供了大量 `mixed`（fetch+send）场景，但其 **only_send 率为 0**，且在 177 条修改类请求中只有 36 条（20%）调用了 send。

**修复方向**：增加 `phase2_dyp.jsonl` 中纯 send 场景的比例。

---

## 三、根因优先级汇总

| 优先级 | 根因 | 数据规模 | 修复工作量 |
|-------|------|---------|-----------|
| **P0** | `zcx_phase2.jsonl` 中 192 条修改请求缺少 send 动作 | 192 条源数据错误，37% TTS-only 率 | 中等（逐条修正 action） |
| **P0** | `zty_2-E.jsonl` 缺失 system prompt，300 条 only_send 丢失 | 300 条，0% 聚合命中率 | 简单（补 system prompt） |
| **P1** | `wsb_phase2.jsonl` 缺少 only_send 覆盖 | 0 条 only_send | 较高（需新增数据） |
| **P1** | `phase2_dyp.jsonl` send 覆盖率仅 20% | 需新增 ~100 条 only_send | 较高（需新增数据） |

---

## 四、修复建议

### 修复 1（高优先级，可执行）：为 `zty_2-E.jsonl` 补上 Phase 2 system prompt

```python
# 当前每条记录的格式（缺少 system prompt）：
[{"role":"user",...}, {"role":"assistant",...}, {"role":"tool",...}]

# 修复后应为：
[{"role":"system","content":"<Phase 2 system prompt>"},
 {"role":"user",...}, {"role":"assistant",...}, {"role":"tool",...}]
```

Phase 2 system prompt 内容参考 `wsb_phase2.jsonl` 或 `zcx_phase2.jsonl` 的第一条 system 消息。补上后重新聚合，预计可增加 300 条高质量 only_send 数据。

### 修复 2（高优先级，需人工审核）：修正 `zcx_phase2.jsonl` 中 192 条缺少 send 的样本

逐条检查 192 条"帮我改X → 无 action"样本，补上 `send_to_ppt_agent` 动作。例如：

```json
// 修复前：
{"role":"assistant","content":"好的。"}
// 修复后：
{"role":"assistant","content":"好的，马上为您修改。<action>send_to_ppt_agent|data:用户要求把第三页配色改成深蓝色。</action>"}
```

### 修复 3（中等优先级）：增加 `wsb_phase2.jsonl` 和 `phase2_dyp.jsonl` 的 only_send 覆盖

参考 `zty_2-E.jsonl` 的模式，在 `wsb_phase2.jsonl` 中新增至少 50 条"用户明确修改请求 → 只 send 不 fetch"的场景。

---

## 五、验证方法

修复后，重新统计聚合文件中：

1. "帮我改X"类请求 → `send_to_ppt_agent` 动作的覆盖率应从 42% 提升至 **85%+**
2. `only_send` 记录数应从 1,504 条提升至 **2,000+ 条**
3. 训练后模型在"帮我改字体 / 帮我加一页案例"等场景下，send 动作调用率应有显著提升
