"""
§3.9.1：判断「悬挂中的新反馈」是否与当前冲突提问相关（仅 LLM，无启发式）。
相关 → 只入队不重播；无关 → 入队并 high 重播。
未配置模型或调用/解析失败时视为「未确认为相关」→ 重播（related=False）。
"""
from __future__ import annotations

import logging
import os
import sys
from pathlib import Path
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from ppt_agent_service.schemas import Intent
    from ppt_agent_service.task_manager import SuspendedPageState

logger = logging.getLogger(__name__)

_REPO_ROOT = Path(__file__).resolve().parents[1]
_PPTAGENT_ROOT = _REPO_ROOT / "PPTAgent"
if str(_PPTAGENT_ROOT) not in sys.path:
    sys.path.insert(0, str(_PPTAGENT_ROOT))


def _classifier_llm_from_env() -> Any:
    if os.getenv("PPT_SUSPEND_RELATED_USE_LLM", "true").lower() not in (
        "1",
        "true",
        "yes",
    ):
        return None
    model_name = (os.getenv("PPT_SUSPEND_RELATED_MODEL") or "").strip() or os.getenv(
        "PPTAGENT_MODEL"
    )
    api_base = os.getenv("PPTAGENT_API_BASE")
    api_key = os.getenv("PPTAGENT_API_KEY")
    if not model_name or not api_key:
        return None
    from pptagent.llms import AsyncLLM

    return AsyncLLM(model_name, api_base, api_key)


async def feedback_related_with_llm(
    sp: Any,
    intents: list[Any],
    raw_text: str,
) -> bool:
    """
    相关 → True（只入队、不重播 high）；无关 → False（入队 + 重播）。
    无 LLM 或失败 → False（重播，避免静默吞掉「无关」提醒）。
    """
    llm = _classifier_llm_from_env()
    if llm is None:
        logger.warning(
            "悬挂相关判定：未配置 LLM（PPT_SUSPEND_RELATED_USE_LLM=false 或缺 PPTAGENT_MODEL/API_KEY），"
            "视为 unrelated → 将重播求助"
        )
        return False

    q = (getattr(sp, "question_for_user", None) or "").strip()[:800]
    r = (getattr(sp, "reason", None) or "").strip()[:400]
    rt = (raw_text or "").strip()[:1200]
    lines = []
    for it in intents:
        lines.append(
            f"- {getattr(it, 'action_type', '')} / {getattr(it, 'target_page_id', '')}: "
            f"{(getattr(it, 'instruction', None) or '')[:300]}"
        )
    intent_block = "\n".join(lines) if lines else "（无）"

    prompt = f"""你是教学课件场景的二元分类器（只输出 JSON，不要 Markdown）。

【系统因编辑冲突向教师提出的问题】
{q}

【内部原因标签】
{r}

【教师新输入 ASR】
{rt}

【解析出的修改意图】
{intent_block}

任务：这条新反馈是否在**回应、澄清或选择**上述冲突问题（与「该问句/该决策」同一话题）？
- 若是（包括在回答选 A 还是 B、确认偏好等）→ related=true
- 若明显在提**无关的新修改**（与上述问题不是一回事）→ related=false

只输出一行 JSON：{{"related": true}} 或 {{"related": false}}"""

    try:
        from pptagent.utils import get_json_from_response

        raw = await llm(
            prompt,
            system_message='只输出形如 {"related": true} 的 JSON。',
            return_json=False,
        )
        if isinstance(raw, tuple):
            raw = raw[0]
        data = get_json_from_response(str(raw))
        if isinstance(data, dict) and "related" in data:
            return bool(data.get("related"))
        logger.warning("悬挂相关判定：LLM 返回无 related 字段，视为 unrelated")
    except Exception as e:
        logger.warning("悬挂相关判定 LLM 失败：%s，视为 unrelated", e)

    return False
