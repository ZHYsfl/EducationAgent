"""
§3.9.3 首行路径：代码不冲突 + 逻辑不冲突 → 不经过 LLM 时的**可规则化** HTML 局部修改。
无法匹配规则时返回 None，由调用方决定是否回退编辑 LLM 或保持原样。
"""
from __future__ import annotations

import re
from typing import Optional


def _norm(s: str) -> str:
    return (s or "").strip()


def try_rule_apply_html(html: str, instruction: str) -> Optional[str]:
    """
    对当前幻灯片 HTML 尝试按自然语言做确定性改写。
    成功返回新 HTML；无法安全执行时返回 None。
    """
    inst = _norm(instruction)
    if not inst or not (html or "").strip():
        return None

    # --- 显式「不改内容」类 ---
    if re.search(
        r"^(保持|维持|不要改|别改|不用改|就这样|可以了|好的|确认)\b",
        inst,
    ):
        return html

    # --- 书名号 / 引号 内文本替换：将「A」改为「B」 ---
    m = re.search(
        r"(?:将|把)\s*「([^」]+)」\s*(?:改(?:成|为)|换(?:成|为))\s*「([^」]+)」",
        inst,
    )
    if m:
        a, b = m.group(1), m.group(2)
        if a in html:
            return html.replace(a, b, 1)
        return None

    m = re.search(
        r'(?:将|把)\s*"([^"]+)"\s*(?:改(?:成|为)|换(?:成|为))\s*"([^"]+)"',
        inst,
    )
    if m:
        a, b = m.group(1), m.group(2)
        if a in html:
            return html.replace(a, b, 1)
        return None

    # --- 标题 ---
    m = re.search(
        r"标题\s*(?:改(?:成|为)|换成|设置为)\s*[「\"]?(.*?)[」\"]?\s*$",
        inst,
    ) or re.search(r"^(?:标题|大标题)\s*[:：]\s*(.+)$", inst)
    if m:
        new_title = _norm(m.group(1))
        if not new_title:
            return None

        def repl_heading(m2: re.Match[str]) -> str:
            return f"{m2.group(1)}{new_title}{m2.group(3)}"

        nh, n = re.subn(
            r"(<h[1-6][^>]*>)(.*?)(</h[1-6]>)",
            repl_heading,
            html,
            count=1,
            flags=re.I | re.S,
        )
        if n:
            return nh
        return f'<h2 style="margin:0 0 12px 0;">{new_title}</h2>' + html

    # --- 简单颜色：标题或文字改为某色 ---
    color_map = [
        ("红色", "#c0392b"),
        ("红", "#c0392b"),
        ("蓝色", "#2980b9"),
        ("蓝", "#2980b9"),
        ("绿色", "#27ae60"),
        ("绿", "#27ae60"),
        ("黑色", "#111111"),
        ("黑", "#111111"),
        ("白色", "#ffffff"),
        ("白", "#ffffff"),
    ]
    for word, hexv in color_map:
        if word in inst and re.search(
            r"(?:标题|文字|字体|字|颜色|改成|改为|换成)", inst
        ):
            nh, n = re.subn(
                r"(<h[1-6])([^>]*>)",
                lambda m2: _inject_color_on_open_tag(m2, hexv),
                html,
                count=1,
                flags=re.I,
            )
            if n:
                return nh
            # 无 h1–h6：给第一个标签加 style
            return re.sub(
                r"^(<[a-zA-Z][a-zA-Z0-9]*)(?=[\s>])",
                rf'\1 style="color:{hexv};"',
                html.strip(),
                count=1,
            )

    # --- 删除「片段」---
    m = re.search(r"删除\s*「([^」]+)」", inst)
    if m:
        frag = m.group(1)
        if len(frag) >= 2 and frag in html:
            return html.replace(frag, "", 1)

    return None


def _inject_color_on_open_tag(m: re.Match[str], hexv: str) -> str:
    tag_start, rest = m.group(1), m.group(2)
    if "style=" in rest.lower():
        inner = re.sub(
            r"style=\"([^\"]*)\"",
            lambda s: "style=\""
            + re.sub(r"color\s*:\s*[^;\"]+;?", "", s.group(1), flags=re.I)
            + f"color:{hexv};"
            + '"',
            rest,
            count=1,
            flags=re.I,
        )
        return f"{tag_start}{inner}"
    return f'{tag_start} style="color:{hexv};"{rest}'
