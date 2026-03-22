"""TaskState / ExportState ↔ JSON，供独立 Database Service 的 internal API 使用。"""
from __future__ import annotations

import json
from typing import Any

from ppt_agent_service.db.payload_codec import dumps_agent_payload, task_from_row_and_payload
from ppt_agent_service.task_manager import ExportState, TaskState, utc_ms


def task_to_persist_wire(task: TaskState) -> dict[str, Any]:
    return {
        "id": task.task_id,
        "session_id": task.session_id,
        "user_id": task.user_id,
        "topic": task.topic,
        "description": task.description or "",
        "total_pages": int(task.total_pages),
        "audience": task.audience or "",
        "global_style": task.global_style or "",
        "status": task.status or "pending",
        "created_at": 0,
        "updated_at": int(task.last_update or utc_ms()),
        "agent_payload": dumps_agent_payload(task),
    }


def task_from_persist_wire(d: dict[str, Any]) -> TaskState:
    ap = d.get("agent_payload")
    if isinstance(ap, dict):
        payload_raw = json.dumps(ap, ensure_ascii=False)
    elif isinstance(ap, str):
        payload_raw = ap
    else:
        payload_raw = "{}"
    ca = int(d.get("created_at") or 0)
    ua = int(d.get("updated_at") or 0)
    if ua <= 0:
        ua = utc_ms()
    return task_from_row_and_payload(
        task_id=str(d.get("id") or ""),
        session_id=str(d.get("session_id") or ""),
        user_id=str(d.get("user_id") or ""),
        topic=str(d.get("topic") or ""),
        description=str(d.get("description") or ""),
        total_pages=int(d.get("total_pages") or 0),
        audience=str(d.get("audience") or ""),
        global_style=str(d.get("global_style") or ""),
        status=str(d.get("status") or "pending"),
        created_at=ca,
        updated_at=ua,
        payload_raw=payload_raw,
    )


def export_to_persist_wire(exp: ExportState) -> dict[str, Any]:
    return {
        "export_id": exp.export_id,
        "task_id": exp.task_id,
        "format": exp.format,
        "status": exp.status,
        "download_url": exp.download_url or "",
        "file_size": int(exp.file_size or 0),
        "last_update": int(exp.last_update or utc_ms()),
    }


def export_from_persist_wire(d: dict[str, Any]) -> ExportState:
    return ExportState(
        export_id=str(d.get("export_id") or ""),
        task_id=str(d.get("task_id") or ""),
        format=str(d.get("format") or "pptx"),
        status=str(d.get("status") or "pending"),
        download_url=str(d.get("download_url") or ""),
        file_size=int(d.get("file_size") or 0),
        last_update=int(d.get("last_update") or 0),
    )
