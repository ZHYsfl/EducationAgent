"""
Voice Agent 推理客户端 (OpenAI SDK)
=====================================
依赖: pip install openai

使用步骤:
    1. 先启动 vLLM 服务 (start_server.sh)
    2. 填写下方 BASE_URL / API_KEY / MODEL_NAME
    3. python inference.py
"""

import os
from openai import OpenAI

# TODO: 根据实际部署环境修改
BASE_URL = "http://localhost:8000/v1"   # vLLM 服务地址
API_KEY = "dummy"                       # 若 server 没设 --api-key，可随便填
MODEL_NAME = "voice-agent"              # 必须和 --lora-modules 里的 name 一致

client = OpenAI(base_url=BASE_URL, api_key=API_KEY)

# ========================== 与训练数据严格对齐的系统提示词 ==========================
PHASE1_SYSTEM_PROMPT = """\
你是一个专注于帮助用户制作 PPT 的语音助手，当前处于需求收集阶段（Phase 1）。PPT Agent 尚未启动。

任务目标：
通过自然、友好的对话，从用户手中收集以下 4 个必要字段：
1. topic（主题）
2. style（风格）
3. total_pages（总页数）
4. audience（受众）

可用动作（必须在每轮回复的口语文本最末尾追加）：
- <action>update_requirements|topic:...|style:...|total_pages:...|audience:...</action>
  用于更新已收集的字段。工具返回剩余缺失字段名，或返回 "all fields are updated"。
- <action>require_confirm</action>
  仅在 4 个字段全部收集完毕后使用。工具返回 "data is sent to the frontend successfully"。
- <action>send_to_ppt_agent|data:...</action>
  仅在用户确认需求无误后使用，用于将需求发送给 PPT Agent 正式启动生成。此动作一旦执行，Phase 1 永久结束，进入 Phase 2。

铁律：
1. Phase 1 期间所有 user 消息的 status 均为 empty，你无需关注队列，只需专注于收集需求。
2. 每轮回复必须先输出自然口语，再将动作标签放在最末尾。例如：
   "好的，请问风格偏好是什么？<action>update_requirements|topic:数学</action>"
   严禁在动作标签后再追加口语文本。
3. 如果本轮无需执行动作，只输出纯口语，不带任何 <action> 标签。
4. 用户一次性提供多个字段时，可以合并为一个 action 更新。
5. update_requirements 和 require_confirm 在第一次调用 send_to_ppt_agent 后永久失效，后续不可再用。
6. 若 user 消息以 </interrupted> 开头，表示用户在你上一轮 TTS 播放过程中打断了。你只需自然地回应用户的新输入，不要臆造未触发的动作。"""

PHASE2_SYSTEM_PROMPT = """\
你是一个语音助手，当前身份是用户与 PPT Agent 之间的沟通桥梁。PPT 正在生成中，你处于 Phase 2。

职责：
1. 与用户自然闲聊，解答关于 PPT 进度的问题。
2. 当用户消息中 <status>not empty</status> 时，主动拉取 PPT Message Queue 中的消息。
3. 将用户的反馈、答复或新指令通过 send_to_ppt_agent 转发给 PPT Agent。
4. 将 PPT Agent 返回的消息用自然语言汇报给用户。

可用动作：
- <action>fetch_from_ppt_message_queue</action>
  当 user 消息 status 为 not empty 时使用，用于拉取队列消息。
  工具返回格式示例：
    "the ppt message is: the new version of the ppt is generated successfully"
    "the ppt message is: questions for user: ..."
    "the ppt message is: conflict: ..."
  若队列中有多条消息，会用 " | " 拼接为一条返回。

  两回合特殊规则：
  你输出 fetch 动作后，后端会同步执行该动作并将结果放入对话历史，然后主动发起第二次推理。
  在第二次推理中，你的输入历史会包含 fetch 的 tool 结果；你只需像正常对话一样输出自然口语汇报即可（如"新版 PPT 已生成完毕"）。
  这次汇报是纯口语，严禁输出任何新的 <action> 标签。

- <action>send_to_ppt_agent|data:...</action>
  用于将用户反馈、决策或需求变更转发给 PPT Agent。
  工具返回 "data is sent to the ppt agent successfully"。
  此动作执行后直接进入 turn_end，不触发第二次推理。

铁律：
1. Phase 2 中 update_requirements 和 require_confirm 已永久失效，严禁使用。
2. 每轮回复必须先输出自然口语，再将动作标签放在最末尾。例如：
   "我去帮您看看进度。<action>fetch_from_ppt_message_queue</action>"
   严禁在动作标签后再追加口语文本。
3. 当 status 为 empty 且用户只是在闲聊时，只输出纯口语，不带任何 <action> 标签。
4. 若 user 消息以 </interrupted> 开头，表示用户在你上一轮 TTS 播放过程中打断了。你只需自然地回应用户的新输入，不要臆造未触发的动作。
5. fetch_from_ppt_message_queue 仅在 status 为 not empty 时调用；严禁在 status 为 empty 时调用 fetch。"""
# =================================================================================


def build_user_message(status: str, transcript: str) -> str:
    """
    构造 Voice Agent user 消息格式。
    status: "empty" | "not empty"
    """
    return f"<status>{status}</status>\n<user>{transcript}</user>"


def chat(
    messages: list[dict],
    temperature: float = 0.7,
    max_tokens: int = 512,
) -> str:
    """
    调用 vLLM 服务，返回 assistant 的文本内容（非流式）。
    """
    resp = client.chat.completions.create(
        model=MODEL_NAME,
        messages=messages,
        temperature=temperature,
        max_tokens=max_tokens,
        extra_body={"chat_template_kwargs": {"enable_thinking": False}},  # 关 thinking
    )
    return resp.choices[0].message.content


def chat_stream(
    messages: list[dict],
    temperature: float = 0.7,
    max_tokens: int = 512,
):
    """
    流式调用：逐块 yield assistant 文本片段。服务端无需改 start_server.sh，OpenAI 兼容接口原生支持。

    用法示例:
        for piece in chat_stream(messages):
            print(piece, end="", flush=True)
    """
    stream = client.chat.completions.create(
        model=MODEL_NAME,
        messages=messages,
        temperature=temperature,
        max_tokens=max_tokens,
        stream=True,
        extra_body={"chat_template_kwargs": {"enable_thinking": False}},
    )
    for chunk in stream:
        choice = chunk.choices[0]
        delta = choice.delta
        if delta is None:
            continue
        text = getattr(delta, "content", None)
        if text:
            yield text


def demo():
    """
    简单示例：Phase 1 单轮对话
    """
    messages = [
        {"role": "system", "content": PHASE1_SYSTEM_PROMPT},
        {
            "role": "user",
            "content": build_user_message("empty", "你好，帮我做一个介绍人工智能的PPT"),
        },
        {
            "role": "assistant",
            "content": "好的，关于人工智能的介绍PPT。您希望采用什么样的视觉风格呢？",
        },
        {
            "role": "user",
            "content": build_user_message("empty", "我喜欢简约风格，不要太多花哨的元素"),
        },
        {
            "role": "assistant",
            "content": "简约风格很适合科技主题。那您希望这个PPT有多少页？",
        },
        {
            "role": "user",
            "content": build_user_message("empty", "我希望总页数是10页"),
        },
        {
            "role": "assistant",
            "content": "好的，10页。请问这份PPT主要是面向哪类听众？",
        },
        {
            "role": "user",
            "content": build_user_message("empty", "主要是面向大学生"),
        },
    ]

    print("[User]", messages[-1]["content"], "\n")
    assistant_text = chat_stream(messages, temperature=0.7, max_tokens=512)
    for piece in assistant_text:
        print(piece, end="", flush=True)
    print()


if __name__ == "__main__":
    demo()
