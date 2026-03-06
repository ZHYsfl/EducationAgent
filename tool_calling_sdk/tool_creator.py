# Meta-Tool: Tool Creator
# 让 Agent 拥有创造工具的能力，动态扩展自己的action空间
# Agent 可以通过这个工具连接到任意 Environment

from tool_calling import Agent, Tool, LLMConfig
from dotenv import load_dotenv
load_dotenv(override=True)
import os
import json

# =============================================================================
# Tool Creator - 创造工具的工具
# =============================================================================

def create_tool_creator(agent: Agent):
    """
    工厂函数：创建一个绑定到特定 agent 的 tool_creator 工具
    通过闭包捕获 agent 引用，让 tool_creator 能够动态添加工具
    """
    
    # 存储已创建的工具，用于管理和回溯
    created_tools_registry = {}
    
    def tool_creator(
        tool_name: str,
        tool_description: str,
        function_code: str,
        parameters_json: str
    ) -> str:
        """
        创造一个新工具并注册到 Agent。
        
        :param tool_name: 工具名称（英文，下划线命名）
        :param tool_description: 工具描述（告诉 Agent 什么时候用这个工具）
        :param function_code: Python 函数代码（必须包含一个函数定义）
        :param parameters_json: JSON 格式的参数 schema
        :return: 创建结果
        """
        print(f"🔧 正在创建新工具: {tool_name}")
        
        # 1. 解析参数 schema
        try:
            parameters = json.loads(parameters_json)
        except json.JSONDecodeError as e:
            return f"❌ 参数 schema 解析失败: {e}"
        
        # 2. 编译函数代码
        # 提供一些常用的导入，让生成的代码更方便
        # exec_globals = {
        #     '__builtins__': __builtins__,
        #     'json': json,
        #     'os': os,
        #     # 可以根据需要添加更多预置模块
        # }
        exec_locals = {}
        
        try:
            exec(function_code, globals(), exec_locals)
        except Exception as e:
            return f"❌ 函数代码编译失败: {e}"
        
        # 3. 查找定义的函数
        func = None
        func_name = None
        for name, obj in exec_locals.items():
            if callable(obj) and not name.startswith('_'):
                func = obj
                func_name = name
                break
        
        if func is None:
            return f"❌ 未在代码中找到函数定义。请确保代码包含 'def function_name(...):' 形式的函数定义。" # tool_creator observation
        
        # 4. 检查是否已存在同名工具
        existing_tool_names = [t.name for t in agent.tools]
        if tool_name in existing_tool_names:
            return f"⚠️ 工具 '{tool_name}' 已存在。如需更新，请先删除旧工具。" # tool_creator observation
        
        # 5. 创建并注册工具
        try:
            new_tool = Tool(
                name=tool_name,
                description=tool_description,
                function=func,
                parameters=parameters
            )
            agent.add_tool(new_tool)
            
            # 记录到注册表
            created_tools_registry[tool_name] = {
                'description': tool_description,
                'function_name': func_name,
                'code': function_code,
                'parameters': parameters
            }
            
            print(f"✅ 工具 '{tool_name}' 创建成功！")
            return f"✅ 工具 '{tool_name}' 创建成功！\n\n" \
                   f"📋 工具信息:\n" \
                   f"- 名称: {tool_name}\n" \
                   f"- 描述: {tool_description}\n" \
                   f"- 函数: {func_name}\n" \
                   f"- 参数: {list(parameters.get('properties', {}).keys())}\n\n" \
                   f"现在你可以使用这个新工具了！" # tool_creator observation
                   
        except Exception as e:
            return f"❌ 工具注册失败: {e}" # tool_creator observation
    
    def list_created_tools() -> str:
        """列出所有通过 tool_creator 创建的工具"""
        if not created_tools_registry:
            return "📭 尚未创建任何自定义工具。" # list_created_tools observation
        
        result = "📦 已创建的自定义工具:\n\n"
        for name, info in created_tools_registry.items():
            result += f"🔧 {name}\n"
            result += f"   描述: {info['description']}\n"
            result += f"   参数: {list(info['parameters'].get('properties', {}).keys())}\n\n"
        return result + "✅ 工具列表已成功列出。" # list_created_tools observation
    
    def delete_tool(tool_name: str) -> str:
        """删除一个已创建的工具"""
        if tool_name not in created_tools_registry:
            return f"❌ 未找到名为 '{tool_name}' 的自定义工具。" # delete_tool observation
        
        # 从 agent 中移除
        agent.tools = [t for t in agent.tools if t.name != tool_name]
        # 从注册表移除
        del created_tools_registry[tool_name]
        
        return f"✅ 工具 '{tool_name}' 已删除。现在工具列表为：{list_created_tools()}" # delete_tool observation  
    
    # 返回三个相关的工具
    return tool_creator, list_created_tools, delete_tool, created_tools_registry


# =============================================================================
# 创建 Tool 对象
# =============================================================================

def build_tool_creator_tools(agent: Agent) -> list[Tool]:
    """
    构建 tool_creator 系列工具并返回 Tool 对象列表
    """
    tool_creator, list_created_tools, delete_tool, registry = create_tool_creator(agent)
    
    # 示例代码，用于 description
    example_code = '''def get_stock_price(symbol: str) -> str:
    # 这里可以调用真实的 API
    return f"股票 {symbol} 的当前价格是 $150.00"'''
    
    example_params = '''{
    "type": "object",
    "properties": {
        "symbol": {
            "type": "string",
            "description": "股票代码，如 AAPL, GOOGL"
        }
    },
    "required": ["symbol"]
}'''
    
    tools = [
        Tool(
            name="create_tool",
            description=(
                "🔧 **元工具：创造新工具**\n\n"
                "当你发现现有工具无法满足需求时，可以使用此工具创造一个新工具。\n"
                "创造的工具会立即可用，扩展你的能力边界。\n\n"
                "**参数说明:**\n"
                "1. `tool_name`: 工具名称（英文，snake_case 命名）\n"
                "2. `tool_description`: 工具描述（清晰说明工具用途）\n"
                "3. `function_code`: Python 函数代码，必须包含完整的函数定义\n"
                "4. `parameters_json`: JSON 格式的参数 schema\n\n"
                f"**示例 - 创建一个获取股票价格的工具:**\n"
                f"```\ntool_name: get_stock_price\n"
                f"function_code:\n{example_code}\n"
                f"parameters_json:\n{example_params}\n```"
            ),
            function=tool_creator,
            parameters={
                "type": "object",
                "properties": {
                    "tool_name": {
                        "type": "string",
                        "description": "工具名称，使用英文和下划线，如 'get_weather', 'calculate_tax'"
                    },
                    "tool_description": {
                        "type": "string",
                        "description": "工具描述，说明工具的用途和使用场景"
                    },
                    "function_code": {
                        "type": "string",
                        "description": "完整的 Python 函数代码，必须包含 def 函数定义"
                    },
                    "parameters_json": {
                        "type": "string",
                        "description": "JSON 格式的参数 schema，定义函数参数的类型和描述"
                    }
                },
                "required": ["tool_name", "tool_description", "function_code", "parameters_json"]
            }
        ),
        Tool(
            name="list_custom_tools",
            description="📦 列出所有通过 create_tool 创建的自定义工具",
            function=list_created_tools,
            parameters={
                "type": "object",
                "properties": {},
                "required": []
            }
        ),
        Tool(
            name="delete_custom_tool",
            description="🗑️ 删除一个通过 create_tool 创建的自定义工具",
            function=delete_tool,
            parameters={
                "type": "object",
                "properties": {
                    "tool_name": {
                        "type": "string",
                        "description": "要删除的工具名称"
                    }
                },
                "required": ["tool_name"]
            }
        )
    ]
    
    return tools


# =============================================================================
# 测试
# =============================================================================

if __name__ == "__main__":
    # 创建 Agent
    agent = Agent(LLMConfig(
        api_key=os.getenv("DEEPSEEK_API_KEY"),
        model="deepseek-chat",
        base_url="https://api.deepseek.com"
    ))
    
    # 添加 tool_creator 系列工具
    tool_creator_tools = build_tool_creator_tools(agent)
    for tool in tool_creator_tools:
        agent.add_tool(tool)
    
    print(f"🚀 Agent 已启动，初始工具数量: {len(agent.tools)}")
    print(f"📋 可用工具: {[t.name for t in agent.tools]}")
    
    # 测试：让 Agent 创建一个新工具并使用它
    observations = [{
        "role": "user", 
        "content": """请帮我创建一个计算斐波那契数列的工具，然后用这个工具计算第 10 个斐波那契数。

要求：
1. 先使用 create_tool 创建一个名为 fibonacci 的工具
2. 然后调用这个新工具计算结果"""
    }]
    
    observations_final = agent.chat(observations)
    
    print(observations_final)
    print(f"\n🔧 最终工具数量: {len(agent.tools)}")
    print(f"📋 最终可用工具: {[t.name for t in agent.tools]}")
