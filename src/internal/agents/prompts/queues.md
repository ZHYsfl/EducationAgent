# 消息队列约定（queue conventions）

本文档不是系统提示词，是双 agent 队列协议的存档说明；运行时系统提示词见同目录
voice_phase1.md / voice_phase2.md / ppt.md（各读 `## 中文版` 一节）。

## 队列

- `VoiceInbox`（voice → ppt）：phase 1 结束时由 `send_to_ppt_agent` 推入需求快照
  JSON；phase 2 中 voice agent 把用户反馈经 `send_to_ppt_agent(content)` 转发进来。
  ppt agent 的 Loop 以它为用户消息源。
- `PPTOutbox`（ppt → voice）：ppt agent 经 `send_to_voice_agent(message)` 推入；
  voice agent 在 phase 2 用 `get_messages_from_ppt_agent` 拉取。即 prompt.md 里的
  `messages_from_ppt_agent_queue`。

## queue_status 状态栏

- 用户消息的**末尾**必须带 `<queue_status>empty|not empty</queue_status>`；
  voice agent 的 compose 与 ppt agent 的 Loop 输入都遵守同一约定，值取对应队列的
  Empty()。
- `not empty` 时接收方必须先拉队列再做别的（ppt prompt 铁律 1；voice phase 2
  职责 2）。
- 人优先（用户说话时）：voice agent 若忙着播 ppt 消息而用户插话，先回应用户，
  ppt 消息等下一轮用 not empty 状态拉取。

## 互发消息的文本格式

- ppt → voice：纯文本状态/结果，`send_to_voice_agent` 直接转发内容。
- voice → ppt（phase 2 转发）：`send_to_ppt_agent(content)`，content 为过滤加工后
  的用户新指令文本。
