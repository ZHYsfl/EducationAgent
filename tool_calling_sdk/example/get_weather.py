import os
import asyncio
import sys
from pathlib import Path

CURRENT_DIR = Path(__file__).resolve().parent
SDK_DIR = CURRENT_DIR.parent
if str(SDK_DIR) not in sys.path:
    sys.path.insert(0, str(SDK_DIR))

from async_tool_calling import Agent, Tool, LLMConfig
from dotenv import load_dotenv
load_dotenv(override=True)

# Tool Environment
def get_weather(city: str) -> str: # Tool Action
    return f"城市 {city} 的天气是晴朗，气温20度，湿度50%，风力2级，空气质量优。" # Tool Observation

async def main():
    config = LLMConfig(
        api_key=os.getenv("DEEPSEEK_API_KEY"),
        model=os.getenv("MODEL"),
        base_url=os.getenv("BASE_URL")
    )
    agent = Agent(config)
    tool = Tool(
        name="get_weather",
        description="获取天气",
        function=get_weather,
        parameters={
            "type": "object",
            "properties": {
                "city": {
                    "type": "string",
                    "description": "城市名称"
                }
            },
            "required": ["city"]
        }
    )
    agent.add_tool(tool)
    observations = [{"role": "user", "content": "请获取北京和杭州各自的天气，并行调用工具get_weather"}]
    observations_final = await agent.chat(observations)
    print(observations_final)

if __name__ == "__main__":
    asyncio.run(main())