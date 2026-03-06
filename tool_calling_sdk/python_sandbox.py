# concrete tool action : python_sandbox_tool, python_figure_sandbox_tool
# provide python sandbox environment for agent to execute python code
# provide python figure sandbox environment for agent to execute python figure code
from matplotlib.figure import Figure
from tool_calling import Agent, Tool, LLMConfig
from dotenv import load_dotenv
load_dotenv(override=True)
import os

# python sandbox tool environment
def python_sandbox_tool(py_code : str, g='globals()') -> str: # python sandbox tool action
    """
    专门用于执行python代码，并获取最终查询或处理结果。
    :param py_code: 字符串形式的Python代码，
    :param g: g，字符串形式变量，表示环境变量，无需设置，保持默认参数即可
    核心作用: 充当代码执行的"环境"或"命名空间" (Namespace)
    :return：代码运行的最终结果
    """    
    print("正在调用python_sandbox_tool工具运行Python代码...")
    try:
        if g is None or isinstance(g, str):
            g = globals()
        return str(eval(py_code, g)) # python sandbox tool observation
    # 若报错，则先测试是否是对相同变量重复赋值
    except Exception as e:
        # 确保g为dict类型
        if g is None or isinstance(g, str):
            g = globals()
        global_vars_before = set(g.keys())
        try:            
            exec(py_code, g)
        except Exception as e:
            return f"代码执行时报错{e}"
        global_vars_after = set(g.keys())
        new_vars = global_vars_after - global_vars_before
        # 若存在新变量
        if new_vars:
            result = {var: g[var] for var in new_vars}
            print("代码已顺利执行，正在进行结果梳理...")
            return f"代码执行成功，新增变量：{result}" # python sandbox tool observation
        else:
            print("代码已顺利执行，正在进行结果梳理...")
            return "已经顺利执行代码" # python sandbox tool observation

python_inter_args = '{"py_code": "import numpy as np\\narr = np.array([1, 2, 3, 4])\\nsum_arr = np.sum(arr)\\nsum_arr"}'
python_sandbox_tool = Tool(
    name="python_sandbox_tool",
    description=f"当用户需要编写Python程序并执行时，请调用该函数。该函数可以执行一段Python代码并返回最终结果，需要注意，本函数只能执行非绘图类的代码，若是绘图相关代码，则需要调用python_figure_sandbox_tool函数运行。\n同时需要注意，编写外部函数的参数消息时，必须是满足json格式的字符串，例如如以下形式字符串就是合规字符串：{python_inter_args}",
    function=python_sandbox_tool,
    parameters={
        "type": "object",
        "properties": {
            "py_code": {
                "type": "string",
                "description": "The Python code to execute."
            },
            "g": {
                "type": "string",
                "description": "运行环境变量，默认保持为 'globals()' 即可。",
                "default": "globals()"
            }
        },
        "required": ["py_code"]
    }
)

# python figure sandbox tool environment
def python_figure_sandbox_tool(py_code: str, fname: str, g='globals()') -> str: # python figure sandbox tool action
    """
    专门用于执行python绘图代码，并获取最终绘图结果。
    :param py_code: 字符串形式的Python代码
    :param fname: 字符串形式的文件名（不含扩展名），同时也用于查找代码中的图像变量
    :param g: g，字符串形式变量，表示环境变量，无需设置，保持默认参数即可
    :return：绘图结果保存路径或错误信息
    """
    print("正在调用python_figure_sandbox_tool工具运行Python绘图代码...")
    import matplotlib
    import matplotlib.pyplot as plt
    import seaborn as sns
    import pandas as pd
    
    # ========== 中文字体配置（避免乱码） ==========
    # 按优先级尝试不同的中文字体：
    # Windows: Microsoft YaHei, SimHei, SimSun
    # macOS: PingFang SC, Heiti SC, STHeiti
    # Linux: WenQuanYi Micro Hei, Noto Sans CJK SC, Droid Sans Fallback
    plt.rcParams['font.sans-serif'] = [
        'Microsoft YaHei',      # Windows 微软雅黑
        'SimHei',               # Windows 黑体
        'PingFang SC',          # macOS 苹方
        'Heiti SC',             # macOS 黑体
        'WenQuanYi Micro Hei',  # Linux 文泉驿微米黑
        'Noto Sans CJK SC',     # Linux/通用 思源黑体
        'DejaVu Sans'           # 兜底英文字体
    ]
    plt.rcParams['axes.unicode_minus'] = False  # 解决负号 '-' 显示为方块的问题
    # =============================================

    # 用于执行代码的本地变量
    local_vars = {"plt": plt, "pd": pd, "sns": sns}

    # 相对路径保存目录
    pics_dir = 'pics'
    if not os.path.exists(pics_dir):
        os.makedirs(pics_dir)

    try:
        # 执行用户代码
        if g is None or isinstance(g, str):
            g = globals()
        exec(py_code, g, local_vars)

        # 获取图像对象
        fig = local_vars.get(fname, None)
        if fig is None and plt.gcf().get_axes():
            fig = plt.gcf()

        if fig and hasattr(fig, 'get_axes') and fig.get_axes():
            rel_path = os.path.join(pics_dir, f"{fname}.png")
            fig.savefig(rel_path, bbox_inches='tight')
            if isinstance(fig, Figure):
                plt.close(fig)
            print("代码已顺利执行，图像已保存。")
            return f"✅ 图片已成功保存至: {rel_path}" # python figure sandbox tool observation
        elif fname in local_vars and not isinstance(local_vars[fname], Figure):
            return f"⚠️ 代码执行成功，但变量 '{fname}' 不是一个有效的 Matplotlib Figure 对象。" # python figure sandbox tool observation
        else:
            # 检查是否直接使用 plt 绑图
            if plt.gcf().get_axes():
                rel_path = os.path.join(pics_dir, f"{fname}.png")
                plt.savefig(rel_path, bbox_inches='tight')
                plt.close(plt.gcf())
                print("代码已顺利执行，使用 plt 直接生成的图像已保存。")
                return f"✅ 图片已成功保存至: {rel_path} (通过 plt 直接保存)" # python figure sandbox tool observation
            else:
                return f"⚠️ 代码执行成功，但未找到有效的图像对象或绘图内容。请确保代码生成了图像并赋值给变量 '{fname}' 或使用了 plt。" # python figure sandbox tool observation

    except Exception as e:
        plt.close('all')
        return f"❌ 执行失败：{e}" # python figure sandbox tool observation

python_figure_sandbox_tool = Tool(
    name="python_figure_sandbox_tool",
    description=("当用户需要使用 Python 进行可视化绘图任务时，请调用该函数。"
                "该函数会执行用户提供的 Python 绘图代码，并自动将生成的图像对象保存为图片文件并展示。\n\n"
                "调用该函数时，请传入以下参数：\n\n"
                "1. `py_code`: 一个字符串形式的 Python 绘图代码，**必须是完整、可独立运行的脚本**，"
                "代码必须创建并返回一个命名为 `fname` 的 matplotlib 图像对象；\n"
                "2. `fname`: 图像对象的变量名（字符串形式），例如 'fig'；\n"
                "3. `g`: 全局变量环境，默认保持为 'globals()' 即可。\n\n"
                "📌 请确保绘图代码满足以下要求：\n"
                "- 包含所有必要的 import（如 `import matplotlib.pyplot as plt`, `import seaborn as sns` 等）；\n"
                "- 必须包含数据定义（如 `df = pd.DataFrame(...)`），不要依赖外部变量；\n"
                "- 推荐使用 `fig, ax = plt.subplots()` 显式创建图像；\n"
                "- 使用 `ax` 对象进行绘图操作（例如：`sns.lineplot(..., ax=ax)`）；\n"
                "- 最后明确将图像对象保存为 `fname` 变量（如 `fig = plt.gcf()`）。\n\n"
                "📌 不需要自己保存图像，函数会自动保存并展示。\n"
                "📌 已内置中文字体支持，可以直接使用中文标题、标签等，无需额外配置字体。\n\n"
                "✅ 合规示例代码：\n"
                "import matplotlib.pyplot as plt\n"
                "import seaborn as sns\n"
                "import pandas as pd\n\n"
                "df = pd.DataFrame({'x': [1, 2, 3], 'y': [4, 5, 6]})\n"
                "fig, ax = plt.subplots()\n"
                "sns.lineplot(data=df, x='x', y='y', ax=ax)\n"
                "ax.set_title('Line Plot')\n"
                "fig = plt.gcf()  # 一定要赋值给 fname 指定的变量名\n"),
    function=python_figure_sandbox_tool,
    parameters={
        "type": "object",
        "properties": {
            "py_code": {
                "type": "string",
                "description": "要执行的Python绑图代码，可用库包括：matplotlib.pyplot, seaborn, pandas"
            },
            "fname": {
                "type": "string",
                "description": "保存图片的文件名（不含扩展名），例如 'my_chart'"
            },
            "g": {
                "type": "string",
                "description": "运行环境变量，默认保持为 'globals()' 即可。",
                "default": "globals()"
            }
        },
        "required": ["py_code", "fname"]
    }
)

if __name__ == "__main__":
    agent = Agent(LLMConfig(
        api_key=os.getenv("DEEPSEEK_API_KEY"),
        model="deepseek-chat",
        base_url="https://api.deepseek.com"
    ))
    agent.add_tool(python_sandbox_tool)
    agent.add_tool(python_figure_sandbox_tool)
    observations = [{"role": "user", "content": "请绘制一个折线图，横坐标为x，纵坐标为y，x从0到10，y为x的平方"}]
    observations_final = agent.chat(observations)
    print(observations_final)