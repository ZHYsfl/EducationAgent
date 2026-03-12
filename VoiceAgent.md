### 我们VoiceAgent的现有方案

音频实时监听，经过VAD，判断是否说话，如果说话，则进行ASR，进行ASR的时候，把用户开始说话前的1个音频块+用户开始说话后的音频内容加起来的内容切成1s1s的独立音频块，经过ASR后，发给Small llm,Small llm是一个用来判断有无打断意图的模型（可以防环境噪音，和语气词），直到SMALL LLM觉得要打断，我们把SMALL LLM认为要打断的音频块+这个音频块前的最多前两个音频块+这个音频块后的直到用户停止说话的音频块拼接起来，发给Large llm，Large llm是一个用来流式生成回答的模型，也是进行TOOL CALLING的模型，直到Large llm生成回答后，流式发给TTS，TTS通过检测标点符号，截断句子，一个句子一个句子的放到队列里，TTS将队列里的句子依次转换为语音播放。使用go语言写这些逻辑，python微调small llm,javascript负责和chrome连接使用chrome强大的回声消除技术。如果用户再次说话打断了，LLM和TTS正在工作，清空LLM和TTS的所有状态和队列，再听用户这次的对话继续工作。

---

### Interactive ReAct 升级方案

在现有方案基础上做四个核心改动：感知层升级为ASR 2pass双轨追踪，思考层实现Multi-Agent异步持续思考，执行层引入动态Channel Sizing自适应优化，前端全新设计为极简科技风格。

#### 感知层（ASR 2pass 双轨）

音频实时监听，经过VAD，判断是否说话，如果说话，则进行ASR（2pass模式），进行ASR的时候，把用户开始说话前的1个音频块+用户开始说话后的音频内容加起来的内容切成1s1s的独立音频块。ASR采用双轨追踪：流式partial结果（2pass-online，低延迟）实时显示在前端并用于draft thinking；用户停止说话后等待2pass-offline的高质量最终结果，优先用final结果作为送给LLM的最终文本，fallback到partial拼接。前端收到final结果时会替换之前的partial显示，并有短暂视觉反馈。ASR结果经过Small LLM判断有无打断意图（过滤环境噪音和语气词）。

#### 思考层（核心改动）

##### 改动1：边听边想（Multi-Agent 异步持续思考）

ASR每识别完一个1s音频块产出文字后，Small LLM判断是否为有意义的输入（过滤纯噪音/语气词）。一旦确认为有意义输入，立刻把当前部分文字送给Large LLM进入预思考模式（draft thinking）。Large LLM在后台流式推理，产出的token不发给TTS，而是保存在draftOutput缓冲区中。

用户继续说话时，ASR继续产出新文字，后续音频块跳过Small LLM（已确认为有意义输入）。系统采用递增间隔策略决定何时重启thinker：首次间隔2个ASR结果，之后每轮递增（2, 3, 4, ...）。重启时cancel上一轮thinker，但**保留其已产出的思考结果**。新一轮thinker启动时，带上更新后的部分文字 + 上一轮thinker的思考输出（作为assistant message传入），并添加约束前缀`[内部草稿，可能不完整或片面，仅供继续推理，不可直接复述]`，在已有推理基础上继续往下想，而非从零开始。没到间隔的时候thinker不被打断，有更多时间深入推理。

递增间隔的设计逻辑：前期用户刚开口，每个词都是新信息，thinker需要较频繁地获取更新后的文本；后期用户意图已基本明确，thinker应当有更长的连续思考时间去深入推理，同时也避免频繁冷启动浪费模型的首token延迟。

等到VAD检测到用户停止说话（vad_end），系统用完整文本 + 最后一轮thinker的全部思考结果一起送给Large LLM做最终回答。此时Large LLM已经有了用户说话整段时间（可能2-5秒）积累的推理基础，首token延迟极低。

##### 改动2：边想边说 + 思考过滤

Large LLM切换到正式回答模式后，开始流式生成token。由于Large LLM是思考模型（如Qwen3），输出中可能包含`<think>...</think>`内部推理块。系统通过流式thinkFilter实时过滤：`<think>`标签内的推理内容被丢弃（不发TTS、不显示前端、不计入对话历史），只有`</think>`之后的可见内容才进入正常处理流程（送TTS、显示前端、存入对话历史）。过滤器支持标签跨token边界切分的鲁棒处理。

系统维护一个token计数器（totalTokens），**计数所有token包括`<think>`内部的token**。这是因为`<think>`内部的token也是模型真实的推理耗时，TokenBudget的目的是衡量模型已经花了多少时间思考，而非产出了多少可见文字。在前50个token内，如果模型已经产出了`</think>`之后的可见内容并且凑成了完整句子，立即送TTS播放。如果到了50个token还没产出可说的内容（模型还在`<think>`里深度推理，或者在做tool calling），系统强制插入一句填充语（如"好的，让我查一下"）立刻送TTS播放，用户听到声音就不会觉得冷场。然后模型继续推理，`</think>`后产出的正式回答直接逐句送TTS，不再额外等待。

注意：draft thinking阶段（边听边想）不过滤`<think>`内容，因为thinker的内部推理正是我们要保留和传递给下一轮thinker的有价值上下文。这些draft内容只作为临时上下文注入下一次LLM调用，不会写入永久对话历史。过滤只在最终回答的输出流上生效。

##### 改动3：打断不清空

如果用户在Large LLM生成回答或TTS播放的过程中再次说话打断了，处理方式改为：停止Large LLM的生成（取消当前stream），清空TTS队列（停止播放），但**不丢弃**Large LLM已经生成的内容。把Large LLM已经生成的部分内容（包括思考过程和已输出的回答片段）作为一条assistant消息追加到对话历史里，标记为被打断。然后把新用户输入作为新的user消息追加到对话历史末尾。下一轮调Large LLM时带上完整的对话历史（包含被打断的那条），Large LLM能看到自己之前想到哪了，可以从断点继续推理，而不是从零重新想。

#### 执行层（不变）

Large LLM流式产出的文本，通过检测标点符号截断成句子，一个句子一个句子放到TTS队列里，TTS将队列里的句子依次转换为语音播放。

#### 动态 Channel Sizing

系统中所有数据通道（音频、ASR结果、句子队列、WebSocket写出、TTS音频块）的缓冲区大小不再硬编码，而是由AdaptiveController动态调整。每轮对话结束时，controller根据各channel的峰值利用率和阻塞次数自动扩缩（利用率>80%扩1.5x，<20%缩0.75x），每个channel有min/max上下限防止极端值。调整后的最优参数持久化到`adaptive_sizes.json`文件，下次冷启动时优先加载上次的最优值，避免从默认值重新摸索。

#### 前端

白底极简科技风（参考OpenAI/OpenRouter风格），对话历史列表（user蓝色竖线、AI灰色竖线的扁平消息块），支持ASR partial实时刷新和final替换高亮。底部可折叠事件日志面板，等宽字体终端风格，记录所有系统事件（VAD、ASR、状态切换等）带毫秒时间戳。

#### 技术栈（不变）

使用Go语言写核心逻辑（感知层事件循环、思考层调度、TTS队列管理），Python微调Small LLM，JavaScript负责和Chrome连接使用Chrome的回声消除技术。