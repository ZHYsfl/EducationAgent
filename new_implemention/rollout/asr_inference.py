import os

from openai import OpenAI

# 与 vllm serve 时的路径一致；不确定时: curl -s http://127.0.0.1:6006/v1/models
BASE = os.environ.get("ASR_OPENAI_BASE_URL", "https://u781083-8452-039e3630.bjb1.seetacloud.com:8443/v1").rstrip("/")
MODEL = os.environ.get("ASR_MODEL_ID", "/root/autodl-tmp/asr")

client = OpenAI(
    base_url=BASE,
    api_key=os.environ.get("OPENAI_API_KEY", "EMPTY"),
)

response = client.chat.completions.create(
    model=MODEL,
    messages=[
        {
            "role": "user",
            "content": [
                {
                    "type": "audio_url",
                    "audio_url": {
                        "url": "https://qianwen-res.oss-cn-beijing.aliyuncs.com/Qwen3-ASR-Repo/asr_en.wav"
                    },
                }
            ],
        }
    ],
)

print(response.choices[0].message.content)
