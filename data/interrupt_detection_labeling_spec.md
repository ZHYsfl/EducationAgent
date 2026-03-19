# 打断检测模型 — 数据标注规范

## 概述

只训练**一个模型（V1）**，V2 的行为通过代码中的忽略词表实现，不需要单独训练。

---

## 模型：V1（只过滤噪音和语气词）

> 几乎所有带语义的内容都打断，只过滤纯噪音和纯语气词。

**interrupt：**
- 完整句子："今天天气怎么样"
- 半句话："我想问"、"那个东西"
- 单字指令："停"、"好"、"对"、"不"
- 确认/否定词："可以"、"不要"、"谢谢"、"再说一遍"
- 语气词+实词："嗯我想问"、"啊对了"

**do not interrupt：**
- 纯语气词："嗯"、"啊"、"哦"、"呃"、"emm"、"额"
- 重复语气词："啊啊啊"、"嗯嗯"、"呃呃"
- 咳嗽/笑声/喷嚏的 ASR 误识别（通常是乱码或单音节）
- 纯空白/无内容
- 环境噪音产生的无意义文字

### System Prompt

```
你是语音意图检测模型。给定一段ASR识别文本，判断其是否包含有意义的用户意图。
interrupt — 文本含有实际语义：问题、指令、陈述、确认、否定、哪怕是未说完的半句话。
do not interrupt — 文本仅为无语义噪声：语气词（嗯、啊、哦、呃、emm）、咳嗽/笑声的误识别、重复填充音、空白或乱码。
只输出 interrupt 或 do not interrupt，不要输出任何其他内容。
```

### 数据配比

| 类别 | 占比 | 示例 |
|------|------|------|
| 纯语气词 → do not interrupt | 25% | 嗯、啊、哦、emm、额 |
| 噪音/乱码 → do not interrupt | 10% | 咳嗽误识别、环境音 |
| 完整句子 → interrupt | 25% | 今天天气怎么样 |
| 半句话 → interrupt | 15% | 我想问、那个 |
| 单字指令 → interrupt | 10% | 停、好、对、不 |
| 语气词+实词 → interrupt | 10% | 嗯我想问、啊对了 |
| 边界 case | 5% | 嗯嗯嗯对、啊？ |

---

## V2：代码词表过滤（不训练模型）

V2 不训练单独的模型，而是在 V1 模型前加一层**硬编码忽略词表**：

```go
var ignoreList = map[string]bool{
    "好": true, "好的": true, "可以": true, "行": true, "行吧": true,
    "对": true, "对的": true, "是": true, "是的": true,
    "OK": true, "ok": true, "没问题": true, "明白": true, "知道了": true, "了解": true,
    "谢谢": true, "感谢": true, "辛苦了": true, "好的谢谢": true,
    "嗯好": true, "嗯对": true, "啊是": true, "哦这样": true, "好吧": true,
    "嗯可以": true, "啊好": true, "嗯嗯好": true,
    "就是说": true, "然后呢": true, "怎么说呢": true, "你知道吧": true, "所以说": true,
}

func isInterruptV2(ctx context.Context, agent *toolcalling.Agent, text string) bool {
    cleaned := strings.TrimSpace(text)
    if ignoreList[cleaned] {
        return false
    }
    return isInterrupt(ctx, agent, cleaned) // V1 模型判断
}
```

**逻辑：**
1. 先查词表：完全匹配 → 直接 do not interrupt，不调用模型
2. 词表没命中 → 交给 V1 模型判断

**实验切换：**
- 用 V1：直接调用 `isInterrupt()`
- 用 V2：调用 `isInterruptV2()`，只需改一行配置

**词表维护：**
- 词表在代码里，随时可以加减，不需要重新训练模型
- 发现新的应该忽略的词 → 加进 `ignoreList`，立即生效

**V1 和 V2 的行为对比：**

| 输入 | V1 | V2 | 原因 |
|------|----|----|------|
| "嗯" | do not interrupt | do not interrupt | V1 模型过滤 |
| "啊啊啊" | do not interrupt | do not interrupt | V1 模型过滤 |
| "好" | interrupt | do not interrupt | 词表命中 |
| "好的" | interrupt | do not interrupt | 词表命中 |
| "谢谢" | interrupt | do not interrupt | 词表命中 |
| "嗯对" | interrupt | do not interrupt | 词表命中 |
| "就是说" | interrupt | do not interrupt | 词表命中 |
| "好，那我问你" | interrupt | interrupt | 词表未命中，V1 模型判断 |
| "嗯，我想知道" | interrupt | interrupt | 词表未命中，V1 模型判断 |
| "停" | interrupt | interrupt | 词表未命中，V1 模型判断 |

---

## Reasoning Content 风格规范

**所有标注者必须遵循统一的 reasoning 格式，不允许自由发挥。**

### 模板

```
[引用] + [分类] + [结论]
```

- **引用**：直接引用用户输入，用单引号包裹
- **分类**：说明属于哪个类别（语气词 / 指令 / 半句话 / 噪音 / ...）
- **结论**：一句话给出判断

### 正确示例

```
'嗯' 是语气词，无语义，不打断。
```

```
'我想问' 是未完成的半句话，含提问意图，打断。
```

```
'嗯，那个微积分' 后半有实际内容，打断。
```

```
'啊？' 带疑问语气，有追问意图，打断。
```

### 错误示例（禁止）

```
❌ 用户说了'嗯'，这是一个典型的无意义填充词/语气词，用户只是在清嗓子或思考，没有实际要表达的内容，不应该打断。
```
→ 太啰嗦，包含揣测（"清嗓子或思考"），不同人会写出完全不同的版本。

```
❌ 这段话虽然以语气词开头，但是后面包含了具有实质性意义的内容表达，因此判断为需要打断。
```
→ 没有引用原文，没有分类，纯废话。

### 规则

1. **reasoning 不超过 30 字**，越短越好
2. **不要揣测用户心理**（"用户可能在思考"、"用户想要..."）
3. **不要解释原因链**，只给分类和结论
4. **引用必须用单引号**，保持格式一致

---

## 数据清洗 Checklist

用大模型统一 reasoning content 时，逐条检查：

- [ ] reasoning 不超过 30 字
- [ ] 以单引号引用用户输入开头
- [ ] 包含明确的分类词（语气词 / 指令 / 半句话 / 噪音）
- [ ] 以"打断"或"不打断"结尾
- [ ] 不含揣测用户心理的描述
- [ ] content 字段严格只有 "interrupt" 或 "do not interrupt"