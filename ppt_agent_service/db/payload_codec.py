"""TaskState ↔ agent_payload JSON（不含 running_job / _merge_locks）。"""
from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Optional

from ppt_agent_service.task_manager import (
    PageMergeState,
    PageState,
    SuspendedPageState,
    TaskState,
    utc_ms,
)


def _page_to_dict(p: PageState) -> dict[str, Any]:
    return {
        "page_id": p.page_id,
        "slide_index": p.slide_index,
        "status": p.status,
        "render_url": p.render_url,
        "py_code": p.py_code,
        "version": p.version,
        "updated_at": p.updated_at,
    }


def _page_from_dict(d: dict[str, Any]) -> PageState:
    return PageState(
        page_id=str(d.get("page_id") or ""),
        slide_index=int(d.get("slide_index") or 0),
        status=str(d.get("status") or "completed"),
        render_url=str(d.get("render_url") or ""),
        py_code=str(d.get("py_code") or ""),
        version=int(d.get("version") or 1),
        updated_at=int(d.get("updated_at") or 0),
    )


def _sp_to_dict(sp: SuspendedPageState) -> dict[str, Any]:
    return {
        "page_id": sp.page_id,
        "context_id": sp.context_id,
        "reason": sp.reason,
        "question_for_user": sp.question_for_user,
        "suspended_at": sp.suspended_at,
        "last_asked_at": sp.last_asked_at,
        "ask_count": sp.ask_count,
        "pending_feedbacks": list(sp.pending_feedbacks),
    }


def _sp_from_dict(d: dict[str, Any]) -> SuspendedPageState:
    pfs = d.get("pending_feedbacks") or []
    if not isinstance(pfs, list):
        pfs = []
    return SuspendedPageState(
        page_id=str(d.get("page_id") or ""),
        context_id=str(d.get("context_id") or ""),
        reason=str(d.get("reason") or ""),
        question_for_user=str(d.get("question_for_user") or ""),
        suspended_at=int(d.get("suspended_at") or 0),
        last_asked_at=int(d.get("last_asked_at") or 0),
        ask_count=int(d.get("ask_count") or 0),
        pending_feedbacks=[x for x in pfs if isinstance(x, dict)],
    )


def _pm_to_dict(pm: PageMergeState) -> dict[str, Any]:
    return {
        "is_running": False,
        "pending_intents": list(pm.pending_intents),
        "chain_baseline_pages": dict(pm.chain_baseline_pages),
        "chain_vad_timestamp": pm.chain_vad_timestamp,
    }


def _pm_from_dict(d: dict[str, Any]) -> PageMergeState:
    pi = d.get("pending_intents") or []
    if not isinstance(pi, list):
        pi = []
    cb = d.get("chain_baseline_pages") or {}
    if not isinstance(cb, dict):
        cb = {}
    return PageMergeState(
        is_running=False,
        pending_intents=[x for x in pi if isinstance(x, dict)],
        chain_baseline_pages={str(k): str(v) for k, v in cb.items()},
        chain_vad_timestamp=int(d.get("chain_vad_timestamp") or 0),
    )


def task_to_agent_payload(task: TaskState) -> dict[str, Any]:
    return {
        "version": task.version,
        "last_update": task.last_update,
        "page_order": list(task.page_order),
        "current_viewing_page_id": task.current_viewing_page_id,
        "pages": {pid: _page_to_dict(p) for pid, p in task.pages.items()},
        "output_pptx_path": str(task.output_pptx_path)
        if task.output_pptx_path
        else None,
        "reference_files": list(task.reference_files),
        "teaching_elements": task.teaching_elements,
        "pending_feedback_lines": list(task.pending_feedback_lines),
        "open_conflict_contexts": dict(task.open_conflict_contexts),
        "suspended_pages": {
            pid: _sp_to_dict(sp) for pid, sp in task.suspended_pages.items()
        },
        "page_merges": {k: _pm_to_dict(pm) for k, pm in task.page_merges.items()},
    }


def task_from_row_and_payload(
    task_id: str,
    session_id: str,
    user_id: str,
    topic: str,
    description: str,
    total_pages: int,
    audience: str,
    global_style: str,
    status: str,
    created_at: int,
    updated_at: int,
    payload_raw: str,
) -> TaskState:
    task = TaskState(
        task_id=task_id,
        user_id=user_id,
        topic=topic,
        description=description,
        total_pages=total_pages,
        audience=audience or "",
        global_style=global_style or "",
        session_id=session_id,
        status=status or "pending",
        last_update=updated_at or utc_ms(),
    )
    if not (payload_raw or "").strip() or payload_raw.strip() == "{}":
        task.last_update = max(task.last_update, updated_at, created_at)
        return task
    try:
        data = json.loads(payload_raw)
    except json.JSONDecodeError:
        task.last_update = max(task.last_update, updated_at, created_at)
        return task
    if not isinstance(data, dict):
        return task

    task.version = int(data.get("version") or task.version)
    task.last_update = int(data.get("last_update") or task.last_update or updated_at)
    task.page_order = [
        str(x) for x in (data.get("page_order") or []) if isinstance(x, str)
    ]
    task.current_viewing_page_id = str(data.get("current_viewing_page_id") or "")

    pages = data.get("pages") or {}
    if isinstance(pages, dict):
        for pid, pd in pages.items():
            if isinstance(pd, dict):
                task.pages[str(pid)] = _page_from_dict(pd)

    opp = data.get("output_pptx_path")
    if isinstance(opp, str) and opp.strip():
        task.output_pptx_path = Path(opp)

    rf = data.get("reference_files")
    if isinstance(rf, list):
        task.reference_files = [x for x in rf if isinstance(x, dict)]

    te = data.get("teaching_elements")
    if isinstance(te, dict):
        task.teaching_elements = te
    elif te is None:
        task.teaching_elements = None

    pfl = data.get("pending_feedback_lines")
    if isinstance(pfl, list):
        task.pending_feedback_lines = [str(x) for x in pfl]

    occ = data.get("open_conflict_contexts")
    if isinstance(occ, dict):
        task.open_conflict_contexts = {
            str(k): str(v) for k, v in occ.items()
        }

    sp = data.get("suspended_pages") or {}
    if isinstance(sp, dict):
        for pid, sd in sp.items():
            if isinstance(sd, dict):
                task.suspended_pages[str(pid)] = _sp_from_dict(sd)

    pm = data.get("page_merges") or {}
    if isinstance(pm, dict):
        for k, md in pm.items():
            if isinstance(md, dict):
                task.page_merges[str(k)] = _pm_from_dict(md)

    # 行上 status 与列一致（列表查询等场景）
    task.status = status or task.status
    return task


def dumps_agent_payload(task: TaskState) -> str:
    return json.dumps(task_to_agent_payload(task), ensure_ascii=False)
