# Notebook Tool - Agent 的动态记事本
# 在 Agent 运行过程中提供临时记忆存储
# 每次运行开始时笔记本为空，运行期间支持增删改查

import sys
from pathlib import Path

CURRENT_DIR = Path(__file__).resolve().parent
SDK_DIR = CURRENT_DIR.parent
if str(SDK_DIR) not in sys.path:
    sys.path.insert(0, str(SDK_DIR))

from async_tool_calling import Agent, Tool, LLMConfig
from dotenv import load_dotenv
load_dotenv(override=True)
import os
from datetime import datetime
import asyncio

# =============================================================================
# Notebook - 动态记事本
# =============================================================================

async def create_notebook_tools(agent: Agent = None):
    """
    工厂函数：创建一个独立的记事本实例
    
    - 调用此函数时创建空笔记本（程序启动时）
    - 运行过程中所有工具函数共享同一个笔记本（通过闭包）
    - 笔记本在整个 Agent 运行期间持久存在
    - 下次重新运行程序才会重置为空
    """
    
    # 笔记本存储：key -> {content, created_at, updated_at}
    notebook = {}
    
    def add_note(key: str, content: str) -> str:
        """添加一条新笔记"""
        if key in notebook:
            return f"⚠️ 笔记 '{key}' 已存在。如需更新，请使用 update_note。" # add_note observation
        
        now = datetime.now().strftime("%H:%M:%S")
        notebook[key] = {
            'content': content,
            'created_at': now,
            'updated_at': now
        }
        return f"✅ 笔记 '{key}' 已添加。\n📝 内容: {content[:100]}{'...' if len(content) > 100 else ''}" # add_note observation
    
    def get_note(key: str) -> str:
        """获取指定笔记的内容"""
        if key not in notebook:
            return f"❌ 未找到笔记 '{key}'。可用的笔记: {list(notebook.keys())}" # get_note observation
        
        note = notebook[key]
        return f"📖 笔记 '{key}':\n{note['content']}\n\n⏰ 创建于: {note['created_at']} | 更新于: {note['updated_at']}" # get_note observation
    
    def update_note(key: str, content: str) -> str:
        """更新已有笔记的内容"""
        if key not in notebook:
            return f"❌ 未找到笔记 '{key}'。请先使用 add_note 创建。" # update_note observation
        
        old_content = notebook[key]['content']
        notebook[key]['content'] = content
        notebook[key]['updated_at'] = datetime.now().strftime("%H:%M:%S")
        
        return f"✅ 笔记 '{key}' 已更新。\n📝 旧内容: {old_content[:50]}{'...' if len(old_content) > 50 else ''}\n📝 新内容: {content[:100]}{'...' if len(content) > 100 else ''}" # update_note observation
    
    def append_note(key: str, content: str) -> str:
        """向已有笔记追加内容"""
        if key not in notebook:
            return f"❌ 未找到笔记 '{key}'。请先使用 add_note 创建。" # append_note observation
        
        notebook[key]['content'] += "\n" + content
        notebook[key]['updated_at'] = datetime.now().strftime("%H:%M:%S")
        
        return f"✅ 已向笔记 '{key}' 追加内容。\n📝 追加: {content[:100]}{'...' if len(content) > 100 else ''}\n📊 当前总长度: {len(notebook[key]['content'])} 字符" # append_note observation
    
    def delete_note(key: str) -> str:
        """删除一条笔记"""
        if key not in notebook:
            return f"❌ 未找到笔记 '{key}'。" # delete_note observation
        
        del notebook[key]
        remaining = list(notebook.keys()) if notebook else "空"
        return f"✅ 笔记 '{key}' 已删除。\n📋 剩余笔记: {remaining}" # delete_note observation
    
    def list_notes() -> str:
        """列出所有笔记的摘要"""
        if not notebook:
            return "📭 笔记本为空。使用 add_note 添加第一条笔记。" # list_notes observation
        
        result = f"📓 笔记本 ({len(notebook)} 条笔记):\n\n"
        for key, note in notebook.items():
            preview = note['content'][:80].replace('\n', ' ')
            if len(note['content']) > 80:
                preview += "..."
            result += f"🔖 [{key}] {preview}\n"
            result += f"   ⏰ {note['updated_at']}\n\n"
        return result # list_notes observation
    
    def get_all_notes() -> str:
        """获取所有笔记的完整内容"""
        if not notebook:
            return "📭 笔记本为空。" # get_all_notes observation
        
        result = f"📓 笔记本完整内容 ({len(notebook)} 条):\n\n"
        result += "=" * 50 + "\n"
        for key, note in notebook.items():
            result += f"📌 [{key}]\n"
            result += "-" * 30 + "\n"
            result += note['content'] + "\n"
            result += "=" * 50 + "\n"
        return result # get_all_notes observation
    
    def clear_notebook() -> str:
        """清空整个笔记本"""
        count = len(notebook)
        notebook.clear()
        return f"🗑️ 笔记本已清空。共删除 {count} 条笔记。" # clear_notebook observation
    
    # 返回所有笔记本操作函数和笔记本引用
    return {
        'add_note': add_note,
        'get_note': get_note,
        'update_note': update_note,
        'append_note': append_note,
        'delete_note': delete_note,
        'list_notes': list_notes,
        'get_all_notes': get_all_notes,
        'clear_notebook': clear_notebook,
        '_notebook': notebook  # 内部引用，用于调试
    }


# =============================================================================
# 创建 Tool 对象
# =============================================================================

async def build_notebook_tools(agent: Agent = None) -> list[Tool]:
    """
    构建笔记本系列工具并返回 Tool 对象列表
    """
    funcs = await create_notebook_tools(agent)
    
    tools = [
        Tool(
            name="add_note",
            description=(
                "📝 添加一条新笔记到笔记本。\n"
                "用于记录重要信息、发现、中间结果等。\n"
                "例如：记录迷宫地图、保存计算中间值、记录探索过的路径等。"
            ),
            function=funcs['add_note'],
            parameters={
                "type": "object",
                "properties": {
                    "key": {
                        "type": "string",
                        "description": "笔记的唯一标识符，如 'map', 'visited_nodes', 'current_state'"
                    },
                    "content": {
                        "type": "string",
                        "description": "笔记内容，可以是任何格式的文本"
                    }
                },
                "required": ["key", "content"]
            }
        ),
        Tool(
            name="get_note",
            description="📖 获取指定笔记的完整内容",
            function=funcs['get_note'],
            parameters={
                "type": "object",
                "properties": {
                    "key": {
                        "type": "string",
                        "description": "要获取的笔记的标识符"
                    }
                },
                "required": ["key"]
            }
        ),
        Tool(
            name="update_note",
            description="✏️ 更新（覆盖）已有笔记的内容",
            function=funcs['update_note'],
            parameters={
                "type": "object",
                "properties": {
                    "key": {
                        "type": "string",
                        "description": "要更新的笔记的标识符"
                    },
                    "content": {
                        "type": "string",
                        "description": "新的笔记内容（会覆盖原内容）"
                    }
                },
                "required": ["key", "content"]
            }
        ),
        Tool(
            name="append_note",
            description="➕ 向已有笔记追加内容（不覆盖原内容）",
            function=funcs['append_note'],
            parameters={
                "type": "object",
                "properties": {
                    "key": {
                        "type": "string",
                        "description": "要追加内容的笔记的标识符"
                    },
                    "content": {
                        "type": "string",
                        "description": "要追加的内容"
                    }
                },
                "required": ["key", "content"]
            }
        ),
        Tool(
            name="delete_note",
            description="🗑️ 删除一条笔记",
            function=funcs['delete_note'],
            parameters={
                "type": "object",
                "properties": {
                    "key": {
                        "type": "string",
                        "description": "要删除的笔记的标识符"
                    }
                },
                "required": ["key"]
            }
        ),
        Tool(
            name="list_notes",
            description="📋 列出笔记本中所有笔记的摘要（标题和预览）",
            function=funcs['list_notes'],
            parameters={
                "type": "object",
                "properties": {},
                "required": []
            }
        ),
        Tool(
            name="get_all_notes",
            description="📓 获取笔记本中所有笔记的完整内容",
            function=funcs['get_all_notes'],
            parameters={
                "type": "object",
                "properties": {},
                "required": []
            }
        ),
        Tool(
            name="clear_notebook",
            description="🧹 清空整个笔记本（谨慎使用）",
            function=funcs['clear_notebook'],
            parameters={
                "type": "object",
                "properties": {},
                "required": []
            }
        )
    ]
    
    return tools


# =============================================================================
# 迷宫环境 - Agent 只能看到局部信息
# =============================================================================

async def create_maze_environment():
    """
    创建一个迷宫环境，Agent 只能通过 look 和 move 工具与之交互
    不能直接看到完整地图，必须探索
    """
    # 迷宫定义（Agent 不知道这个）
    # 0 = 可通行, 1 = 墙壁
    maze = [
        [0, 0, 1, 0],
        [1, 0, 0, 0],
        [0, 0, 1, 0],
        [1, 0, 0, 0]
    ]
    rows, cols = len(maze), len(maze[0])
    start = (0, 0)
    goal = (3, 3)
    
    # Agent 当前位置
    state = {'pos': list(start), 'steps': 0}
    
    def look() -> str:
        """观察当前位置和可移动方向"""
        r, c = state['pos']
        result = f"📍 当前位置: ({r}, {c})\n"
        result += f"🚶 已走步数: {state['steps']}\n\n"
        
        # 检查是否到达终点
        if (r, c) == goal:
            result += "🎉 你已经到达出口！迷宫探索成功！\n"
            return result # look observation
        
        # 检查四个方向
        directions = {
            'up': (-1, 0),
            'down': (1, 0),
            'left': (0, -1),
            'right': (0, 1)
        }
        
        result += "👀 可移动方向:\n"
        available = []
        for name, (dr, dc) in directions.items():
            nr, nc = r + dr, c + dc
            if 0 <= nr < rows and 0 <= nc < cols:
                if maze[nr][nc] == 0:
                    available.append(name)
                    result += f"  ✅ {name} -> ({nr}, {nc})\n"
                else:
                    result += f"  🧱 {name} -> 墙壁\n"
            else:
                result += f"  🚫 {name} -> 边界外\n"
        
        if not available:
            result += "\n⚠️ 没有可移动的方向，你被困住了！\n"
        
        return result # look observation
    
    def move(direction: str) -> str:
        """向指定方向移动"""
        r, c = state['pos']
        
        directions = {
            'up': (-1, 0),
            'down': (1, 0),
            'left': (0, -1),
            'right': (0, 1)
        }
        
        if direction not in directions:
            return f"❌ 无效方向 '{direction}'。可用方向: up, down, left, right" # move observation
        
        dr, dc = directions[direction]
        nr, nc = r + dr, c + dc
        
        # 检查边界
        if not (0 <= nr < rows and 0 <= nc < cols):
            return f"❌ 无法向 {direction} 移动，超出边界！" # move observation
        
        # 检查墙壁
        if maze[nr][nc] == 1:
            return f"❌ 无法向 {direction} 移动，前方是墙壁！" # move observation
        
        # 移动成功
        state['pos'] = [nr, nc]
        state['steps'] += 1
        
        result = f"✅ 成功向 {direction} 移动！\n"
        result += f"📍 新位置: ({nr}, {nc})\n"
        
        # 检查是否到达终点
        if (nr, nc) == goal:
            result += f"\n🎉🎉🎉 恭喜！你找到了出口！\n"
            result += f"📊 总步数: {state['steps']}\n"
        
        return result # move observation
    
    return look, move, state


async def build_maze_tools() -> list[Tool]:
    """构建迷宫探索工具"""
    look, move, state = await create_maze_environment()
    
    tools = [
        Tool(
            name="look",
            description="👀 观察当前位置，查看坐标和可移动的方向",
            function=look,
            parameters={
                "type": "object",
                "properties": {},
                "required": []
            }
        ),
        Tool(
            name="move",
            description="🚶 向指定方向移动一步（up/down/left/right）",
            function=move,
            parameters={
                "type": "object",
                "properties": {
                    "direction": {
                        "type": "string",
                        "enum": ["up", "down", "left", "right"],
                        "description": "移动方向"
                    }
                },
                "required": ["direction"]
            }
        )
    ]
    return tools


# =============================================================================
# 测试
# =============================================================================

async def main():
    # 创建 Agent
    agent = Agent(LLMConfig(
        api_key=os.getenv("DEEPSEEK_API_KEY"),
        model=os.getenv("MODEL"),
        base_url=os.getenv("BASE_URL")
    ))
    
    # 添加笔记本工具（用于记录探索过程）
    notebook_tools = await build_notebook_tools(agent)
    for tool in notebook_tools:
        agent.add_tool(tool)
    
    # 添加迷宫探索工具
    maze_tools = await build_maze_tools()
    for tool in maze_tools:
        agent.add_tool(tool)
    
    print(f"🚀 Agent 已启动，工具数量: {len(agent.tools)}")
    print(f"📋 可用工具: {[t.name for t in agent.tools]}")
    
    # 测试：真正的迷宫探索（Agent 不知道全局地图）
    observations = [{
        "role": "user", 
        "content": """你现在在一个迷宫中，需要找到出口。

规则：
1. 你不知道迷宫的完整地图，只能通过 look 工具查看当前位置和可移动方向
2. 使用 move 工具移动（up/down/left/right）
3. 强烈建议使用笔记本（add_note, append_note）记录你探索过的位置和发现，避免走回头路
4. 找到出口（系统会提示你到达）

开始探索吧！先用 look 看看周围环境。"""
    }]
    
    observations_final = await agent.chat(observations)
    
    print("\n" + "="*60)
    print("📊 探索结果:")
    print("="*60)
    for obs in observations_final:
        role = obs.get('role', 'unknown')
        content = obs.get('content', '')
        if content:
            print(f"\n[{role}]: {content[:1000]}{'...' if len(str(content)) > 1000 else ''}")

if __name__ == "__main__":
    asyncio.run(main())