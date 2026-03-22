"""
§3.8 三路合并与 LLM 冲突裁决（MergeResult）。
"""
from __future__ import annotations

import difflib
import logging
import os
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Optional

_REPO_ROOT = Path(__file__).resolve().parents[1]
_PPTAGENT_ROOT = _REPO_ROOT / "PPTAgent"
if str(_PPTAGENT_ROOT) not in sys.path:
    sys.path.insert(0, str(_PPTAGENT_ROOT))

from pptagent.utils import get_json_from_response  # noqa: E402

logger = logging.getLogger(__name__)


@dataclass
class MergeDecision:
    merge_status: str  # auto_resolved | ask_human
    merged_pycode: str = ""
    question_for_user: str = ""
    #: True 表示走 §3.9.3 首行：合并裁决未调用 LLM（纯规则判定）
    rule_merge_path: bool = False


def build_system_patch(
    snapshot_pages: Optional[dict[str, Any]],
    page_id: str,
    current_py_code: str,
) -> str:
    """V_base → V_current 的 unified diff 文本摘要（§3.8.1 system_patch）。"""
    if not snapshot_pages or not page_id:
        return ""
    entry = snapshot_pages.get(page_id) or {}
    base_code = str(entry.get("py_code", "") or "")
    if not base_code.strip():
        return "(VAD 快照中无该页 py_code，无法计算 diff)"
    a = base_code.splitlines()
    b = current_py_code.splitlines()
    diff = list(
        difflib.unified_diff(a, b, fromfile="V_base", tofile="V_current", lineterm="")
    )
    text = "\n".join(diff)
    if not text.strip():
        return "(无文本行级差异)"
    return text[:12000]


def build_system_patch_global(
    snapshot_pages: Optional[dict[str, Any]],
    page_order: list[str],
    current_concat: str,
) -> str:
    """
    §3.8.1 global_modify：V_base / V_current 均为按 page_order 拼接的整册 py_code，再算 diff。
    """
    if not page_order:
        return "(无 VAD 快照，system_patch 为空)"
    parts: list[str] = []
    for pid in page_order:
        if not isinstance(pid, str):
            continue
        ent = (snapshot_pages or {}).get(pid) or {}
        parts.append(str(ent.get("py_code", "") or ""))
    base_concat = "\n".join(parts)
    if not base_concat.strip():
        return "(无 VAD 快照，system_patch 为空)"
    a = base_concat.splitlines()
    b = (current_concat or "").splitlines()
    diff = list(
        difflib.unified_diff(
            a, b, fromfile="V_base_global", tofile="V_current_global", lineterm=""
        )
    )
    text = "\n".join(diff)
    if not text.strip():
        return "(无文本行级差异)"
    return text[:12000]


def _patch_is_no_code_conflict(system_patch: str) -> bool:
    """
    「代码不冲突」：V_base 与 V_current 无实质文本差异，或尚无可用快照对比。
    """
    p = (system_patch or "").strip()
    if not p:
        return True
    if p == "(无文本行级差异)":
        return True
    if p == "(无 VAD 快照，system_patch 为空)":
        return True
    # 无法算 diff 时保守视为存在代码不确定性，走 LLM 裁决
    if "VAD 快照中无该页" in p or "无法计算 diff" in p:
        return False
    # 有 unified_diff 内容 → 代码已漂移
    if p.lstrip().startswith("---") or "\n@@" in p or "\n+" in p or "\n-" in p:
        return False
    # 其它说明性括号文案
    if p.startswith("(") and p.endswith(")"):
        return True
    return False


def _instruction_is_logic_conflict(instruction: str) -> bool:
    """「逻辑冲突」：多选、自相矛盾、强歧义等，需 ask_human 或 LLM 裁决。"""
    inst = (instruction or "").strip()
    if not inst:
        return False
    keys = (
        "冲突",
        "矛盾",
        "二选一",
        "要么",
        "或者",
        "要不要",
        "是否",
        "哪个",
        "哪种",
        "更好",
        "可以吗",
        "行不行",
        "选哪个",
        "哪一个",
        "删不删",
        "留不留",
        "整课",
        "全部删掉",
    )
    if any(k in inst for k in keys):
        return True
    # 「A还是B」式二选一（避免单用「还是」如「还是改成X吧」误伤）
    if re.search(r"[\w\u4e00-\u9fff]\s*还是\s*[\w\u4e00-\u9fff]", inst):
        return True
    # 多个强疑问句
    if inst.count("？") >= 2:
        return True
    if inst.count("?") >= 2:
        return True
    return False


def _merge_llm_from_env():
    use = os.getenv("PPT_MERGE_USE_LLM", "true").lower() in ("1", "true", "yes")
    if not use:
        return None
    model_name = os.getenv("PPTAGENT_MODEL")
    api_base = os.getenv("PPTAGENT_API_BASE")
    api_key = os.getenv("PPTAGENT_API_KEY")
    if not model_name or not api_key:
        return None
    from pptagent.llms import AsyncLLM

    return AsyncLLM(model_name, api_base, api_key)


async def decide_three_way_merge(
    *,
    task_id: str,
    page_id: str,
    current_code: str,
    system_patch: str,
    instruction: str,
    action_type: str,
    llm: Any = None,
) -> MergeDecision:
    """
    §3.9.3 三条路径：
    - 代码不冲突 + 逻辑不冲突 → 直接 auto_resolved，**不调用合并 LLM**（rule_merge_path=True）
    - 代码冲突 + 逻辑不冲突 → 三路合并 **LLM** → auto_resolved
    - 代码冲突 + 逻辑冲突 → 三路合并 **LLM** → ask_human（或启发式）
    """
    inst = (instruction or "").strip()
    patch = (system_patch or "").strip()

    code_ok = _patch_is_no_code_conflict(patch)
    logic_ok = not _instruction_is_logic_conflict(inst)

    # ---------- 首行：纯规则，不经过合并 LLM ----------
    if code_ok and logic_ok:
        return MergeDecision(
            merge_status="auto_resolved",
            merged_pycode="",
            question_for_user="",
            rule_merge_path=True,
        )

    # ---------- 代码不冲突但逻辑冲突 → 问人 ----------
    if code_ok and not logic_ok:
        return MergeDecision(
            merge_status="ask_human",
            merged_pycode="",
            question_for_user="您的说法里有多重选择或需要确认的点，请用一句话说明您希望保留哪一种做法？",
            rule_merge_path=False,
        )

    llm = llm or _merge_llm_from_env()
    if llm is None:
        return _heuristic_merge(inst, patch)

    prompt = f"""你是课件编辑「三路合并」裁决器（教学场景）。
任务 task_id={task_id}，页面 page_id={page_id}，用户操作类型 action_type={action_type}。

【V_current 页面 Python 源码（节选）】
{current_code[:6000]}

【V_base→V_current 的 system_patch（diff 摘要）】
{patch[:4000]}

【用户自然语言指令】
{inst}

请输出**仅一个** JSON 对象，键为：
- merge_status: 字符串 "auto_resolved" 或 "ask_human"
- merged_pycode: 当 auto_resolved 时，给出合并用户指令后的**完整**页面 Python 源码（必须保留 def get_slide_markup）；若无法在不猜用户偏好下完成则必须用 ask_human
- question_for_user: 当 ask_human 时，一两句口语化中文问句，让用户通过语音二选一或澄清；auto_resolved 时为空字符串
"""

    try:
        raw = await llm(
            prompt,
            system_message="只输出 JSON，不要 Markdown。",
            return_json=False,
        )
        if isinstance(raw, tuple):
            raw = raw[0]
        data = get_json_from_response(str(raw))
        status = str(data.get("merge_status", "ask_human")).strip()
        if status not in ("auto_resolved", "ask_human"):
            status = "ask_human"
        return MergeDecision(
            merge_status=status,
            merged_pycode=str(data.get("merged_pycode", "") or ""),
            question_for_user=str(data.get("question_for_user", "") or ""),
            rule_merge_path=False,
        )
    except Exception as e:
        # §0.2 50210：LLM 依赖失败时降级启发式（后台合并无 HTTP 响应体）
        logger.warning("三路合并 LLM 调用失败（业务码 50210）：%s", e)
        return _heuristic_merge(inst, patch)


def _heuristic_merge(inst: str, patch: str) -> MergeDecision:
    if _instruction_is_logic_conflict(inst):
        return MergeDecision(
            merge_status="ask_human",
            question_for_user="课件这一页的改法有两种可能，您更希望保留哪一种？请简单说下您的选择。",
            rule_merge_path=False,
        )
    if len(patch) > 2000 and any(
        k in inst for k in ("风格", "模板", "全盘", "全部", "整体")
    ):
        return MergeDecision(
            merge_status="ask_human",
            question_for_user="您的修改和当前版本差异较大，需要确认是整体替换风格还是只改这一处？",
            rule_merge_path=False,
        )
    return MergeDecision(
        merge_status="auto_resolved",
        merged_pycode="",
        rule_merge_path=False,
    )
