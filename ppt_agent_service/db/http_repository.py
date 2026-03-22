"""通过 HTTP 调用独立 Database Service（§0.7 端口 9500）的 internal API。"""
from __future__ import annotations

import logging
from typing import Any, Optional

import httpx

from ppt_agent_service.db.dto import SessionSummary, TaskListRow
from ppt_agent_service.db.wire_format import (
    export_from_persist_wire,
    export_to_persist_wire,
    task_from_persist_wire,
    task_to_persist_wire,
)
from ppt_agent_service.task_manager import ExportState, TaskState

logger = logging.getLogger(__name__)


def _ensure_business_ok(data: Any) -> None:
    """Database Service 可能以 HTTP 200 + JSON code!=200 返回业务错误（与 PPT Agent 一致）。"""
    if not isinstance(data, dict):
        return
    c = data.get("code")
    if c is not None and int(c) != 200:
        raise RuntimeError(str(data.get("message") or "database service 业务错误"))


class HTTPPPTRepository:
    """与 PPTRepository 方法签名兼容，供 TaskManager 注入。"""

    def __init__(self, base_url: str, internal_key: str = ""):
        self._base = base_url.rstrip("/")
        self._key = (internal_key or "").strip()

    def _headers(self) -> dict[str, str]:
        h: dict[str, str] = {}
        if self._key:
            h["X-Internal-Key"] = self._key
        return h

    async def ensure_user(self, user_id: str, *, display_name: str = "") -> None:
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.post(
                f"{self._base}/internal/db/ensure-user",
                json={"user_id": user_id, "display_name": display_name},
                headers=self._headers(),
            )
            r.raise_for_status()

    async def ensure_session(
        self, session_id: str, user_id: str, *, title: str = ""
    ) -> None:
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.post(
                f"{self._base}/internal/db/ensure-session",
                json={
                    "session_id": session_id,
                    "user_id": user_id,
                    "title": title,
                },
                headers=self._headers(),
            )
            r.raise_for_status()

    async def create_session(self, user_id: str, title: str = "") -> str:
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.post(
                f"{self._base}/internal/db/create-session",
                json={"user_id": user_id, "title": title},
                headers=self._headers(),
            )
            r.raise_for_status()
            data = r.json()
            _ensure_business_ok(data)
            return str(data.get("session_id") or "")

    async def get_session(self, session_id: str) -> Optional[SessionSummary]:
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.get(
                f"{self._base}/internal/db/sessions/{session_id}",
                headers=self._headers(),
            )
            if r.status_code == 404:
                return None
            r.raise_for_status()
            d = r.json()
            return SessionSummary(
                session_id=str(d.get("session_id") or ""),
                user_id=str(d.get("user_id") or ""),
                title=str(d.get("title") or ""),
                status=str(d.get("status") or "active"),
                created_at=int(d.get("created_at") or 0),
                updated_at=int(d.get("updated_at") or 0),
            )

    async def list_sessions_paged(
        self, user_id: str, page: int, page_size: int
    ) -> tuple[list[SessionSummary], int]:
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.get(
                f"{self._base}/internal/db/sessions",
                params={"user_id": user_id, "page": page, "page_size": page_size},
                headers=self._headers(),
            )
            r.raise_for_status()
            data = r.json()
            items = [
                SessionSummary(
                    session_id=str(x.get("session_id") or ""),
                    user_id=str(x.get("user_id") or ""),
                    title=str(x.get("title") or ""),
                    status=str(x.get("status") or "active"),
                    created_at=int(x.get("created_at") or 0),
                    updated_at=int(x.get("updated_at") or 0),
                )
                for x in (data.get("items") or [])
            ]
            return items, int(data.get("total") or 0)

    async def update_session(
        self,
        session_id: str,
        *,
        title: Optional[str] = None,
        status: Optional[str] = None,
    ) -> bool:
        payload: dict[str, Any] = {}
        if title is not None:
            payload["title"] = title
        if status is not None:
            payload["status"] = status
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.patch(
                f"{self._base}/internal/db/sessions/{session_id}",
                json=payload,
                headers=self._headers(),
            )
            if r.status_code == 404:
                return False
            r.raise_for_status()
            return bool(r.json().get("ok", True))

    async def save_task(self, task: TaskState) -> None:
        body = task_to_persist_wire(task)
        async with httpx.AsyncClient(timeout=120.0) as client:
            r = await client.put(
                f"{self._base}/internal/db/tasks/{task.task_id}",
                json=body,
                headers=self._headers(),
            )
            r.raise_for_status()

    async def load_task(self, task_id: str) -> Optional[TaskState]:
        tid = (task_id or "").strip()
        if not tid:
            return None
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.get(
                f"{self._base}/internal/db/tasks/{tid}",
                headers=self._headers(),
            )
            if r.status_code == 404:
                return None
            r.raise_for_status()
            return task_from_persist_wire(r.json())

    async def list_tasks_light(
        self,
        session_id: str,
        page: int,
        page_size: int,
        user_id: Optional[str] = None,
    ) -> tuple[list[TaskListRow], int]:
        params: dict[str, Any] = {
            "session_id": session_id,
            "page": page,
            "page_size": page_size,
        }
        if user_id:
            params["user_id"] = user_id
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.get(
                f"{self._base}/internal/db/task-list",
                params=params,
                headers=self._headers(),
            )
            r.raise_for_status()
            data = r.json()
            rows = [
                TaskListRow(
                    task_id=str(x.get("task_id") or ""),
                    session_id=str(x.get("session_id") or ""),
                    user_id=str(x.get("user_id") or ""),
                    topic=str(x.get("topic") or ""),
                    status=str(x.get("status") or "pending"),
                    updated_at=int(x.get("updated_at") or 0),
                )
                for x in (data.get("items") or [])
            ]
            return rows, int(data.get("total") or 0)

    async def save_export(self, exp: ExportState) -> None:
        body = export_to_persist_wire(exp)
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.put(
                f"{self._base}/internal/db/exports/{exp.export_id}",
                json=body,
                headers=self._headers(),
            )
            r.raise_for_status()

    async def load_export(self, export_id: str) -> Optional[ExportState]:
        eid = (export_id or "").strip()
        if not eid:
            return None
        async with httpx.AsyncClient(timeout=60.0) as client:
            r = await client.get(
                f"{self._base}/internal/db/exports/{eid}",
                headers=self._headers(),
            )
            if r.status_code == 404:
                return None
            r.raise_for_status()
            return export_from_persist_wire(r.json())
