"""
§3.8–§3.9 三路合并、悬挂页时序、冲突→Voice 编排。
"""
from __future__ import annotations

import asyncio
import json
import os
from collections.abc import Awaitable, Callable
from typing import Any, Optional
from uuid import uuid4

from ppt_agent_service.canvas_redis import CanvasRedis, get_py_code_from_canvas_doc
from ppt_agent_service.merge_engine import (
    build_system_patch,
    build_system_patch_global,
    decide_three_way_merge,
)
from ppt_agent_service.rule_merge_apply import try_rule_apply_html
from ppt_agent_service.schemas import Intent, PPTFeedbackRequest
from ppt_agent_service.suspension_related import feedback_related_with_llm
from ppt_agent_service.slide_py_code import (
    extract_slide_html_from_py,
    wrap_slide_html_as_python,
)
from ppt_agent_service.task_manager import (
    PageMergeState,
    PageState,
    SuspendedPageState,
    TaskManager,
    TaskState,
    utc_ms,
)
from ppt_agent_service.voice_client import voice_ppt_message

GLOBAL_KEY = "__global__"
SUSPEND_REASK_SEC = int(os.getenv("PPT_SUSPEND_REASK_SEC", "45"))
SUSPEND_AUTORESOLVE_SEC = int(os.getenv("PPT_SUSPEND_AUTORESOLVE_SEC", "180"))
# §3.9.3 首行规则合并未命中可执行规则时，是否仍调用编辑 LLM（默认 true 兼顾体验；false 则保持 V_current）
_APPLY_LLM_FALLBACK = os.getenv("PPT_393_APPLY_LLM_FALLBACK", "true").lower() in (
    "1",
    "true",
    "yes",
)

ScheduleRegen = Callable[[str], Awaitable[None]]
_schedule_regen: Optional[ScheduleRegen] = None

PartialRefresh = Callable[[str, list[str]], Awaitable[None]]
_partial_refresh: Optional[PartialRefresh] = None

_slide_editor: Any = None


def set_schedule_regeneration(fn: ScheduleRegen) -> None:
    global _schedule_regen
    _schedule_regen = fn


def set_partial_refresh(fn: PartialRefresh) -> None:
    global _partial_refresh
    _partial_refresh = fn


def set_slide_editor(editor: Any) -> None:
    global _slide_editor
    _slide_editor = editor


def new_context_id() -> str:
    # §0.4：ctx_ 前缀 + UUID v4
    return f"ctx_{uuid4()}"


def merge_bucket(intent: Intent) -> str:
    if intent.action_type == "global_modify":
        return GLOBAL_KEY
    return (intent.target_page_id or "").strip() or GLOBAL_KEY


async def get_current_code_for_merge(
    canvas: CanvasRedis,
    task: TaskState,
    bucket: str,
) -> tuple[str, str]:
    """
    §3.9.2：V_current 优先 Redis `canvas:{task_id}` 的 pages[*].py_code，缺页再回落内存。
    返回 (current_code, page_id_for_system_patch)。
    """
    doc = await canvas.load_canvas_document(task.task_id) or {}
    order = doc.get("page_order") or list(task.page_order)
    if not isinstance(order, list):
        order = list(task.page_order)

    if bucket == GLOBAL_KEY:
        parts: list[str] = []
        for pid in order:
            if not isinstance(pid, str):
                continue
            code = get_py_code_from_canvas_doc(doc, pid)
            if not code.strip():
                p = task.pages.get(pid)
                if p:
                    code = p.py_code
            parts.append(code)
        current = "\n".join(parts)
        if not current.strip() and task.pages:
            current = "\n".join(
                task.pages[pid].py_code for pid in task.page_order if pid in task.pages
            )
        page_for_patch = _suspend_page_id_for_bucket(task, GLOBAL_KEY)
        return current, page_for_patch

    code = get_py_code_from_canvas_doc(doc, bucket)
    if not code.strip():
        p = task.pages.get(bucket)
        if p:
            code = p.py_code
    return code, bucket


def _suspend_page_id_for_bucket(task: TaskState, bucket: str) -> str:
    if bucket == GLOBAL_KEY:
        if task.current_viewing_page_id and task.current_viewing_page_id in task.pages:
            return task.current_viewing_page_id
        if task.page_order:
            return task.page_order[0]
        return ""
    return bucket


async def suspend_watch_loop(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    page_id: str,
) -> None:
    """§3.9.1：45s 重问 → 3min 自决。"""
    try:
        await asyncio.sleep(SUSPEND_REASK_SEC)
        task = await tm.get_task(task_id)
        if not task or page_id not in task.suspended_pages:
            return
        sp = task.suspended_pages[page_id]
        sp.last_asked_at = utc_ms()
        sp.ask_count += 1
        await voice_ppt_message(
            task_id=task_id,
            page_id=page_id,
            priority="high",
            context_id=sp.context_id,
            tts_text=(
                sp.question_for_user
                or "仍在等待您对上一问题的确认，请回答以便继续修改课件。"
            ),
            msg_type="conflict_question",
        )
        await asyncio.sleep(max(0.0, SUSPEND_AUTORESOLVE_SEC - SUSPEND_REASK_SEC))
        task = await tm.get_task(task_id)
        if not task or page_id not in task.suspended_pages:
            return
        await auto_resolve_suspension(tm, canvas, task_id, page_id)
    except asyncio.CancelledError:
        raise


async def auto_resolve_suspension(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    page_id: str,
) -> None:
    """§3.9.1 超时自决：解除悬挂后优先 LLM 局部改页 + 刷新资源。"""
    task = await tm.get_task(task_id)
    if not task or page_id not in task.suspended_pages:
        return
    sp = task.suspended_pages.pop(page_id)
    await tm.cancel_suspend_watcher(task_id, page_id)
    task.open_conflict_contexts.pop(sp.context_id, None)
    if page_id in task.pages:
        task.pages[page_id].status = "completed"
    note = (
        f"\n\n[系统自决-悬挂超时{PPT_SUSPEND_AUTORESOLVE_SEC}s page={page_id} "
        f"context_id={sp.context_id}]\n采用系统自动优化方案。原因摘要：{sp.reason}\n"
    )
    for pb in sp.pending_feedbacks:
        note += f"[pending_feedback_while_suspended] {json.dumps(pb, ensure_ascii=False)}\n"
    busy = task.running_job is not None and not task.running_job.done()
    if busy:
        await tm.append_pending_feedback(task_id, [note])
    else:
        task.description += note
    task.last_update = utc_ms()
    if not busy and page_id in task.pages:
        if _slide_editor is not None:
            try:
                html = extract_slide_html_from_py(task.pages[page_id].py_code)
                new_html = await _slide_editor.edit_slide_html_llm(
                    current_html=html,
                    instruction=note,
                    action_type="system_auto_resolve",
                    topic=task.topic,
                )
                p = task.pages[page_id]
                p.py_code = wrap_slide_html_as_python(
                    new_html, page_id=page_id, slide_index=p.slide_index
                )
                p.version += 1
                p.updated_at = utc_ms()
            except Exception:
                pass
        elif _schedule_regen:
            await _schedule_regen(task_id)
    if not busy and _partial_refresh and page_id in task.pages and _slide_editor is not None:
        await _partial_refresh(task_id, [page_id])
    await canvas.save_canvas_from_task(task)
    await tm.persist_task(task_id)


async def enter_suspension(
    tm: TaskManager,
    canvas: CanvasRedis,
    task: TaskState,
    suspend_on_page_id: str,
    question: str,
    reason: str,
) -> None:
    if not suspend_on_page_id or suspend_on_page_id not in task.pages:
        suspend_on_page_id = _suspend_page_id_for_bucket(task, GLOBAL_KEY)
    if not suspend_on_page_id:
        return
    ctx = new_context_id()
    now = utc_ms()
    sp = SuspendedPageState(
        page_id=suspend_on_page_id,
        context_id=ctx,
        reason=reason,
        question_for_user=question,
        suspended_at=now,
        last_asked_at=now,
        ask_count=1,
        pending_feedbacks=[],
    )
    task.suspended_pages[suspend_on_page_id] = sp
    await tm.register_open_conflict(task.task_id, ctx, suspend_on_page_id)
    task.pages[suspend_on_page_id].status = "suspended_for_human"
    task.last_update = now
    await canvas.save_canvas_from_task(task)
    await voice_ppt_message(
        task_id=task.task_id,
        page_id=suspend_on_page_id,
        priority="high",
        context_id=ctx,
        tts_text=question or "课件编辑需要您确认一个问题，请回答。",
        msg_type="conflict_question",
    )
    tm.schedule_suspend_watcher(
        task.task_id,
        suspend_on_page_id,
        suspend_watch_loop(tm, canvas, task.task_id, suspend_on_page_id),
    )
    await tm.persist_task(task.task_id)


def _html_from_merge_decision(decision: Any, current_html: str) -> str:
    mpy = (getattr(decision, "merged_pycode", None) or "").strip()
    if not mpy:
        return current_html
    h = extract_slide_html_from_py(mpy)
    return h if h.strip() else current_html


async def _one_page_new_html(
    task: TaskState,
    p: PageState,
    intent: Intent,
    decision: Any,
) -> Optional[str]:
    """
    返回新 HTML；None 表示无法局部编辑应整册重跑。
    §3.9.3：rule_merge_path 时优先规则改写 HTML，不调用合并 LLM；是否再调编辑 LLM 见 PPT_393_APPLY_LLM_FALLBACK。
    """
    current_html = extract_slide_html_from_py(p.py_code)
    merged_html = _html_from_merge_decision(decision, current_html)
    if merged_html != current_html:
        return merged_html

    if getattr(decision, "rule_merge_path", False):
        ruled = try_rule_apply_html(current_html, intent.instruction)
        if ruled is not None:
            return ruled
        if not _APPLY_LLM_FALLBACK:
            return current_html

    if _slide_editor is None:
        return None
    return await _slide_editor.edit_slide_html_llm(
        current_html=current_html,
        instruction=intent.instruction,
        action_type=intent.action_type,
        topic=task.topic,
    )


async def _apply_merge_decision_to_pages(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    bucket: str,
    decision: Any,
    intent: Intent,
) -> None:
    """auto_resolved：规则改写或编辑 LLM 改 HTML + 刷新预览/pptx，不调用 PPTAgent。"""
    task = await tm.get_task(task_id)
    if not task or decision.merge_status != "auto_resolved":
        return
    busy = task.running_job is not None and not task.running_job.done()
    line = f"[partial_edit bucket={bucket} action={intent.action_type}] {intent.instruction}"
    if busy:
        await tm.append_pending_feedback(task_id, [line])
        return

    changed: list[str] = []

    async def apply_pid(pid: str) -> bool:
        p = task.pages.get(pid)
        if not p:
            return True
        new_html = await _one_page_new_html(task, p, intent, decision)
        if new_html is None:
            return False
        p.py_code = wrap_slide_html_as_python(
            new_html, page_id=pid, slide_index=p.slide_index
        )
        p.version += 1
        p.updated_at = utc_ms()
        changed.append(pid)
        return True

    if bucket == GLOBAL_KEY:
        for pid in list(task.page_order):
            ok = await apply_pid(pid)
            if not ok:
                task.description += (
                    "\n[partial_edit_fallback_full] 无可用编辑模型，触发整册重生成\n"
                )
                if _schedule_regen:
                    await _schedule_regen(task_id)
                return
    else:
        ok = await apply_pid(bucket)
        if not ok:
            task.description += (
                f"\n[partial_edit_fallback_full page={bucket}] "
                f"{intent.instruction}\n"
            )
            if _schedule_regen:
                await _schedule_regen(task_id)
            return

    task.description += "\n" + line + "\n"
    task.last_update = utc_ms()

    # §3.9.2：链式合并的 V_base ← 本轮输出
    pm = task.page_merges.setdefault(bucket, PageMergeState())
    if bucket == GLOBAL_KEY:
        for pid in list(task.page_order):
            if pid in task.pages:
                pm.chain_baseline_pages[pid] = task.pages[pid].py_code
    else:
        if bucket in task.pages:
            pm.chain_baseline_pages[bucket] = task.pages[bucket].py_code

    await canvas.save_canvas_from_task(task)
    if _partial_refresh and changed:
        await _partial_refresh(task_id, changed)
    await tm.persist_task(task_id)


async def process_one_intent(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    intent: Intent,
    base_timestamp: int,
    raw_text: str,
    *,
    chain_continuation: bool = False,
) -> None:
    task = await tm.get_task(task_id)
    if not task:
        return

    if intent.action_type in ("insert_before", "insert_after", "delete"):
        line = (
            f"[structural action={intent.action_type} "
            f"target={intent.target_page_id}] {intent.instruction}"
        )
        busy = task.running_job is not None and not task.running_job.done()
        if busy:
            await tm.append_pending_feedback(task_id, [line])
        else:
            task.description += "\n" + line + "\n"
            task.last_update = utc_ms()
        if not busy and _schedule_regen:
            await _schedule_regen(task_id)
        await tm.persist_task(task_id)
        return

    bucket = merge_bucket(intent)
    pm = task.page_merges.setdefault(bucket, PageMergeState())

    if bucket != GLOBAL_KEY and bucket not in task.pages:
        task.description += f"\n[merge skip: missing page {bucket}]\n"
        task.last_update = utc_ms()
        await tm.persist_task(task_id)
        return

    current, page_for_patch = await get_current_code_for_merge(canvas, task, bucket)

    # §3.9.2：V_base = 上一轮合并结果（链式）；否则 = 当前 VAD 快照
    base_pages: Optional[dict[str, Any]] = None
    if chain_continuation and pm.chain_baseline_pages:
        base_pages = {
            pid: {"page_id": pid, "py_code": code, "status": "completed"}
            for pid, code in pm.chain_baseline_pages.items()
        }
    elif chain_continuation and pm.chain_vad_timestamp > 0:
        snap = await canvas.load_snapshot_document(
            task_id, pm.chain_vad_timestamp
        )
        base_pages = (snap or {}).get("pages") if snap else None
    elif base_timestamp > 0:
        snap = await canvas.load_snapshot_document(task_id, base_timestamp)
        base_pages = (snap or {}).get("pages") if snap else None

    if bucket == GLOBAL_KEY:
        patch = (
            build_system_patch_global(base_pages, list(task.page_order), current)
            if base_pages
            else "(无 VAD 快照，system_patch 为空)"
        )
    else:
        patch = (
            build_system_patch(base_pages, page_for_patch, current)
            if base_pages
            else "(无 VAD 快照，system_patch 为空)"
        )

    decision = await decide_three_way_merge(
        task_id=task_id,
        page_id=bucket,
        current_code=current,
        system_patch=patch,
        instruction=intent.instruction,
        action_type=intent.action_type,
    )

    if decision.merge_status == "ask_human":
        pid = _suspend_page_id_for_bucket(task, bucket)
        await enter_suspension(
            tm,
            canvas,
            task,
            pid,
            decision.question_for_user or "需要您确认如何修改课件。",
            f"merge ask_human bucket={bucket} action={intent.action_type}",
        )
        return

    await _apply_merge_decision_to_pages(tm, canvas, task_id, bucket, decision, intent)


async def drain_pending_merge_intents(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    bucket_key: str,
) -> None:
    task = await tm.get_task(task_id)
    if not task:
        return
    if bucket_key in task.suspended_pages:
        return
    if bucket_key == GLOBAL_KEY and task.suspended_pages:
        return
    pm = task.page_merges.setdefault(bucket_key, PageMergeState())
    if not pm.pending_intents:
        return
    raw = pm.pending_intents.pop(0)
    it = Intent(
        action_type=raw["action_type"],
        target_page_id=raw.get("target_page_id", ""),
        instruction=raw.get("instruction", ""),
    )
    rt = str(raw.get("raw_text", "") or "")
    await run_merge_intent_serial(
        tm,
        canvas,
        task_id,
        it,
        0,
        rt,
        chain_continuation=True,
    )


async def run_merge_intent_serial(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    intent: Intent,
    base_timestamp: int,
    raw_text: str,
    *,
    chain_continuation: bool = False,
) -> None:
    """§3.9.2：同 bucket 串行 + 合并中到达的意图入队。"""
    task = await tm.get_task(task_id)
    if not task:
        return
    bucket = merge_bucket(intent)
    lock = task.merge_lock(bucket)
    async with lock:
        pm = task.page_merges.setdefault(bucket, PageMergeState())
        if pm.is_running:
            d = intent.model_dump()
            d["base_timestamp"] = base_timestamp
            d["raw_text"] = raw_text
            pm.pending_intents.append(d)
            return
        if not chain_continuation:
            if base_timestamp > 0:
                if pm.chain_vad_timestamp not in (0, base_timestamp):
                    pm.chain_baseline_pages.clear()
                pm.chain_vad_timestamp = base_timestamp
        pm.is_running = True
    try:
        await process_one_intent(
            tm,
            canvas,
            task_id,
            intent,
            base_timestamp,
            raw_text,
            chain_continuation=chain_continuation,
        )
    finally:
        async with lock:
            task = await tm.get_task(task_id)
            if task:
                pm = task.page_merges.setdefault(bucket, PageMergeState())
                pm.is_running = False
        await drain_pending_merge_intents(tm, canvas, task_id, bucket)
        await tm.persist_task(task_id)


async def run_feedback_merge_job(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    intents: list[Intent],
    base_timestamp: int,
    raw_text: str,
) -> None:
    for it in intents:
        await run_merge_intent_serial(
            tm, canvas, task_id, it, base_timestamp, raw_text
        )


def start_merge_background(
    tm: TaskManager,
    canvas: CanvasRedis,
    task_id: str,
    intents: list[Intent],
    base_ts: int,
    raw_text: str,
) -> None:
    asyncio.create_task(
        run_feedback_merge_job(tm, canvas, task_id, intents, base_ts, raw_text)
    )


async def feedback_targets_suspended_page(task: TaskState, intents: list[Intent]) -> bool:
    suspended = set(task.suspended_pages.keys())
    for it in intents:
        if it.action_type == "global_modify":
            continue
        pid = (it.target_page_id or "").strip()
        if pid and pid in suspended:
            return True
    return False


async def queue_intents_on_suspended_and_reask(
    tm: TaskManager,
    canvas: CanvasRedis,
    task: TaskState,
    intents: list[Intent],
    raw_text: str = "",
) -> None:
    """§3.9.1：仅「与悬挂无关」的反馈入队并 high 重发求助；相关则只入队不重播。"""
    suspended = set(task.suspended_pages.keys())
    target_sp: Optional[SuspendedPageState] = None
    target_pid = ""
    for it in intents:
        if it.action_type == "global_modify":
            continue
        pid = (it.target_page_id or "").strip()
        if pid in suspended:
            target_pid = pid
            target_sp = task.suspended_pages[pid]
            break
    if not target_sp or not target_pid:
        return
    for it in intents:
        target_sp.pending_feedbacks.append(it.model_dump())
    task.last_update = utc_ms()
    related = await feedback_related_with_llm(target_sp, intents, raw_text)
    if not related:
        await voice_ppt_message(
            task_id=task.task_id,
            page_id=target_pid,
            priority="high",
            context_id=target_sp.context_id,
            tts_text=target_sp.question_for_user
            or "您有新的修改说明；请先确认上一问题，或一并说明您的偏好。",
            msg_type="conflict_question",
        )
    await canvas.save_canvas_from_task(task)
    await tm.persist_task(task.task_id)


async def handle_resolve_conflict_branch(
    tm: TaskManager,
    canvas: CanvasRedis,
    task: TaskState,
    req: PPTFeedbackRequest,
) -> tuple[str, Optional[str]]:
    """
    返回 ("skip"|"ok"|"err", message)。
    skip：请求中无 resolve_conflict。
    """
    if not any(it.action_type == "resolve_conflict" for it in req.intents):
        return "skip", None
    ctx = (req.reply_to_context_id or "").strip()
    if not ctx:
        return "err", "resolve_conflict 必须携带 reply_to_context_id"
    pid = await tm.clear_open_conflict(task.task_id, ctx)
    if not pid:
        return "err", "无待解决的冲突上下文（context_id 无效或已过期）"
    await tm.cancel_suspend_watcher(task.task_id, pid)
    sp = task.suspended_pages.pop(pid, None)
    task.open_conflict_contexts.pop(ctx, None)
    if pid in task.pages:
        task.pages[pid].status = "completed"
    lines: list[str] = []
    for it in req.intents:
        if it.action_type == "resolve_conflict":
            lines.append(
                f"[resolve_conflict context_id={ctx} target={it.target_page_id}] "
                f"{it.instruction}"
            )
    if sp:
        for pb in sp.pending_feedbacks:
            lines.append(
                "[pending_while_suspended] "
                + json.dumps(pb, ensure_ascii=False)
            )
    block = "\n\n[教师反馈修改]\n" + "\n".join(lines) + "\n"
    busy = task.running_job is not None and not task.running_job.done()
    if busy:
        await tm.append_pending_feedback(task.task_id, [block])
    else:
        task.description += block
    task.last_update = utc_ms()
    if not busy and pid in task.pages:
        if _slide_editor is not None:
            try:
                instr = "\n".join(lines)
                html = extract_slide_html_from_py(task.pages[pid].py_code)
                new_html = await _slide_editor.edit_slide_html_llm(
                    current_html=html,
                    instruction=instr,
                    action_type="resolve_conflict",
                    topic=task.topic,
                )
                p = task.pages[pid]
                p.py_code = wrap_slide_html_as_python(
                    new_html, page_id=pid, slide_index=p.slide_index
                )
                p.version += 1
                p.updated_at = utc_ms()
            except Exception:
                pass
        elif _schedule_regen:
            await _schedule_regen(task.task_id)
    if not busy and _partial_refresh and pid in task.pages and _slide_editor is not None:
        await _partial_refresh(task.task_id, [pid])
    await canvas.save_canvas_from_task(task)

    pending_for_merge: list[Intent] = []
    if sp:
        for pb in sp.pending_feedbacks:
            if not isinstance(pb, dict):
                continue
            try:
                pending_for_merge.append(
                    Intent(
                        action_type=str(pb.get("action_type") or "modify"),
                        target_page_id=str(pb.get("target_page_id") or ""),
                        instruction=str(pb.get("instruction") or ""),
                    )
                )
            except Exception:
                continue
    if pending_for_merge and not busy:
        start_merge_background(
            tm,
            canvas,
            task.task_id,
            pending_for_merge,
            req.base_timestamp,
            req.raw_text or "",
        )
    await tm.persist_task(task.task_id)
    return "ok", None
