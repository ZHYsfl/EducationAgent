"""
§3.1 / §3.7：py_code 字段为「当前页面的 Python 源码」。
生成管线仍产出 HTML；此处将 HTML 封装为可执行、可合并的 Python 模块片段，并支持还原为 HTML。
"""
from __future__ import annotations

import ast

SLIDE_MARKUP_FN = "get_slide_markup"


def wrap_slide_html_as_python(
    html: str,
    *,
    page_id: str,
    slide_index: int,
) -> str:
    """
    生成合法 Python 源码：`get_slide_markup() -> str` 返回本页 HTML。
    使用 repr(html) 保证任意字符均可嵌入，无需手工转义三引号。
    """
    payload = repr(html if html is not None else "")
    return (
        "# EducationAgent PPT Agent — slide Python source (spec: py_code)\n"
        f"# page_id: {page_id}\n"
        f"# slide_index: {slide_index}\n"
        "\n"
        f"def {SLIDE_MARKUP_FN}() -> str:\n"
        '    """Return slide HTML markup for canvas / renderer."""\n'
        f"    return {payload}\n"
    )


def extract_slide_html_from_py(py_code: str) -> str:
    """
    从规范形态的 py_code 中取出 HTML；若为历史数据（整段即 HTML），原样返回。
    """
    if not py_code or not py_code.strip():
        return ""
    if f"def {SLIDE_MARKUP_FN}" not in py_code:
        return py_code

    try:
        tree = ast.parse(py_code)
    except SyntaxError:
        return py_code

    for node in tree.body:
        if isinstance(node, ast.FunctionDef) and node.name == SLIDE_MARKUP_FN:
            for stmt in node.body:
                if isinstance(stmt, ast.Return) and stmt.value is not None:
                    try:
                        return ast.literal_eval(stmt.value)
                    except (ValueError, TypeError, SyntaxError):
                        break
            break

    return py_code
