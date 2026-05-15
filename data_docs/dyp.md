# dyp.jsonl & phase2_dyp.jsonl 数据占比分析报告

> 分析日期：2026-05-14

---

# 第一部分：dyp.jsonl（Phase 1 数据）

> 数据文件：`dyp.jsonl`
> 数据总量：750 条

## 一、数据总览

| 类型 | 数量 | 占比 |
|------|------|------|
| 无 action（纯对话） | 450 条 | 60% |
| **有 action（Phase 1）** | **300 条** | **40%** |

无 action 的 450 条纯对话中，打断与未打断各占一半：未打断 225 条，单次打断 225 条。

---

## 二、Phase 1 有 action 数据细分

| 类别 | 描述 | 数量 | 占比 |
|------|------|------|------|
| **示例 A** | 有 action，**未打断** | 0 条 | 0% |
| **示例 B** | 有 action，**1 次打断** | 216 条 | 72% |
| **示例 C** | 有 action，**3 次打断** | 84 条 | 28% |
| **合计** | — | **300 条** | 100% |

---

## 三、示例 B 内部结构

- **打断时机**：全部在第 1 轮 assistant 说话时被打断
- **对话轮数**：
  - 4 轮：33 条
  - 5 轮：27 条
  - 6 轮：156 条
- **update_requirements**：全部 216 条均为一次性更新 4 个字段
- **require_confirm**：216 条（100%）
- **send_to_ppt_agent**：216 条（100%）

---

## 四、示例 C 内部结构

- **打断时机**：第 1、2、3 轮连续被打断（均为 3 次打断）
- **对话轮数**：全部为 6 轮
- **action 延迟**：第一 action 延迟至第 4 轮 assistant 才发出
- **update_requirements**：全部 84 条均为一次性更新 4 个字段
- **require_confirm**：84 条（100%）
- **send_to_ppt_agent**：84 条（100%）

---

# 第二部分：phase2_dyp.jsonl（Phase 2 数据）

> 数据文件：`phase2_dyp .jsonl`
> 总行数：467 行（注释行 13 行 + 有效记录 450 条）

## 五、数据总览

| 类型 | 数量 | 占比 |
|------|------|------|
| **有效记录** | **450 条** | 100% |
| 注释行 | 13 条 | — |

所有 450 条记录均为 Phase 2 数据。

---

## 六、按 action 类型分类

| 类别 | 描述 | 数量 | 占比 |
|------|------|------|------|
| **fetch + send 混合** | 先 fetch 队列消息，再根据反馈 send 给 PPT Agent | 376 条 | 83.6% |
| **只 fetch** | 仅拉取队列消息，不发送反馈 | 74 条 | 16.4% |
| **只 send** | — | 0 条 | 0% |
| **纯对话（无 action）** | — | 0 条 | 0% |

---

## 七、对话打断分布

| 类型 | 数量 | 占比 |
|------|------|------|
| **未打断** | 300 条 | 66.7% |
| **有打断** | 150 条 | 33.3% |

---

## 八、user 消息 status 分布

| 类型 | 数量 |
|------|------|
| **empty** | 174 条（user 消息数） |
| **not empty** | 914 条（user 消息数） |

> 注：not empty 的 user 消息数量多于记录总数，说明同一记录中可有多个 user 消息。

---

## 九、对话轮数分布（user 消息数）

| 轮数 | 数量 | 占比 |
|------|------|------|
| 2 轮 | 382 条 | 84.9% |
| 3 轮 | 16 条 | 3.6% |
| 4 轮 | 14 条 | 3.1% |
| 5 轮 | 16 条 | 3.6% |
| 6 轮 | 14 条 | 3.1% |
| 7 轮 | 8 条 | 1.8% |

---

## 十、tool 消息内容类型

| 类型 | 数量 |
|------|------|
| **conflict（冲突/疑问）** | 150 条 |
| **conflict + questions for user** | 130 条 |
| **混合消息（三种消息类型均有）** | 170 条 |
| **send_to_ppt 成功** | 376 条 |

---

## 十一、独立汇报 TTS（fetch 之后的纯口语汇报）

| 类型 | 数量 | 占比 |
|------|------|------|
| **有汇报 TTS** | 450 条 | 100% |

---

## 十二、按数据注释分类汇总

---

2-76：not interrupted chat dataset(include action) : queue is not empty : the ppt message is:something is wrong, the situation is:conflict

---

79-153：not interrupted chat dataset(include action) : queue is not empty : the ppt message is: mixed

---

156-305：not interrupted chat dataset(include action) : queue is not empty : the ppt message is: mixed

---

308-322：interrupted chat dataset(include action) : queue is not empty : the ppt message is:something is wrong, the situation is:conflict  一次打断 (15条)

---

324-338：interrupted chat dataset(include action) : queue is not empty : the ppt message is:something is wrong, the situation is:conflict  两次打断 (15条)

---

340-354：interrupted chat dataset(include action) : queue is not empty : the ppt message is:something is wrong, the situation is:conflict  三次打断 (15条)

---

356-370：interrupted chat dataset(include action) : queue is not empty : the ppt message is:something is wrong, the situation is:conflict  四次打断 (15条)

---

372-386：interrupted chat dataset(include action) : queue is not empty : the ppt message is:something is wrong, the situation is:conflict  五次打断 (15条)

---

389-403：interrupted chat dataset(include action) : queue is not empty :the ppt message is:mixed   一次打断（15条）

---

405-419：interrupted chat dataset(include action) : queue is not empty :the ppt message is:mixed   二次打断（15条）

---

421-435：interrupted chat dataset(include action) : queue is not empty :the ppt message is:mixed   三次打断（15条）

---

437-451：interrupted chat dataset(include action) : queue is not empty :the ppt message is:mixed   四次打断（15条）

---

453-467：interrupted chat dataset(include action) : queue is not empty :the ppt message is:mixed   五次打断（15条）
