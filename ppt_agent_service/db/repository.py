"""§7.2 users/sessions/tasks 持久化与 §7.3.7–7.3.14 查询支撑。"""
from __future__ import annotations

import logging
from typing import Optional
from uuid import uuid4

from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from ppt_agent_service.db.dto import SessionSummary, TaskListRow
from ppt_agent_service.db.models import ExportRow, SessionRow, TaskRow, UserRow
from ppt_agent_service.db.payload_codec import dumps_agent_payload, task_from_row_and_payload
from ppt_agent_service.task_manager import ExportState, TaskState, utc_ms

logger = logging.getLogger(__name__)


def new_session_id() -> str:
    """§0.4：sess_ + UUID v4（标准带横杠）。"""
    return f"sess_{uuid4()}"


class PPTRepository:
    def __init__(self, factory: async_sessionmaker[AsyncSession]):
        self._factory = factory

    async def ensure_user(
        self,
        user_id: str,
        *,
        display_name: str = "",
    ) -> None:
        uid = (user_id or "").strip()
        if not uid:
            return
        now = utc_ms()
        async with self._factory() as s:
            row = await s.get(UserRow, uid)
            if row is not None:
                return
            s.add(
                UserRow(
                    id=uid,
                    username=uid,
                    email=f"{uid}@ppt-agent.local",
                    password_hash="$noop$",
                    display_name=(display_name or uid)[:128],
                    subject="",
                    school="",
                    role="teacher",
                    created_at=now,
                    updated_at=now,
                )
            )
            await s.commit()

    async def ensure_session(
        self,
        session_id: str,
        user_id: str,
        *,
        title: str = "",
    ) -> None:
        sid = (session_id or "").strip()
        uid = (user_id or "").strip()
        if not sid or not uid:
            return
        await self.ensure_user(uid)
        now = utc_ms()
        async with self._factory() as s:
            row = await s.get(SessionRow, sid)
            if row is not None:
                if row.user_id != uid:
                    logger.warning(
                        "session %s 已存在且归属 user %s，忽略 ensure 为 %s",
                        sid,
                        row.user_id,
                        uid,
                    )
                return
            s.add(
                SessionRow(
                    id=sid,
                    user_id=uid,
                    title=(title or "")[:256],
                    status="active",
                    created_at=now,
                    updated_at=now,
                )
            )
            await s.commit()

    async def create_session(self, user_id: str, title: str = "") -> str:
        uid = (user_id or "").strip()
        if not uid:
            raise ValueError("user_id 不能为空")
        await self.ensure_user(uid)
        sid = new_session_id()
        now = utc_ms()
        async with self._factory() as s:
            s.add(
                SessionRow(
                    id=sid,
                    user_id=uid,
                    title=(title or "")[:256],
                    status="active",
                    created_at=now,
                    updated_at=now,
                )
            )
            await s.commit()
        return sid

    async def get_session(self, session_id: str) -> Optional[SessionSummary]:
        sid = (session_id or "").strip()
        if not sid:
            return None
        async with self._factory() as s:
            row = await s.get(SessionRow, sid)
            if row is None:
                return None
            return SessionSummary(
                session_id=row.id,
                user_id=row.user_id,
                title=row.title or "",
                status=row.status or "active",
                created_at=row.created_at,
                updated_at=row.updated_at,
            )

    async def list_sessions_paged(
        self,
        user_id: str,
        page: int,
        page_size: int,
    ) -> tuple[list[SessionSummary], int]:
        uid = (user_id or "").strip()
        p = max(1, page)
        ps = max(1, min(page_size, 100))
        offset = (p - 1) * ps
        async with self._factory() as s:
            cnt_q = select(func.count()).select_from(SessionRow).where(
                SessionRow.user_id == uid
            )
            total = int((await s.execute(cnt_q)).scalar_one())
            q = (
                select(SessionRow)
                .where(SessionRow.user_id == uid)
                .order_by(SessionRow.updated_at.desc())
                .offset(offset)
                .limit(ps)
            )
            rows = (await s.execute(q)).scalars().all()
        items = [
            SessionSummary(
                session_id=r.id,
                user_id=r.user_id,
                title=r.title or "",
                status=r.status or "active",
                created_at=r.created_at,
                updated_at=r.updated_at,
            )
            for r in rows
        ]
        return items, total

    async def update_session(
        self,
        session_id: str,
        *,
        title: Optional[str] = None,
        status: Optional[str] = None,
    ) -> bool:
        sid = (session_id or "").strip()
        if not sid:
            return False
        now = utc_ms()
        async with self._factory() as s:
            row = await s.get(SessionRow, sid)
            if row is None:
                return False
            if title is not None:
                row.title = title[:256]
            if status is not None:
                row.status = status[:16]
            row.updated_at = now
            await s.commit()
        return True

    async def save_task(self, task: TaskState) -> None:
        await self.ensure_session(task.session_id, task.user_id)
        now = utc_ms()
        payload = dumps_agent_payload(task)
        async with self._factory() as s:
            row = await s.get(TaskRow, task.task_id)
            if row is None:
                row = TaskRow(
                    id=task.task_id,
                    session_id=task.session_id,
                    user_id=task.user_id,
                    topic=task.topic[:256],
                    description=task.description or "",
                    total_pages=int(task.total_pages),
                    audience=(task.audience or "")[:128],
                    global_style=(task.global_style or "")[:128],
                    status=task.status or "pending",
                    created_at=now,
                    updated_at=now,
                    agent_payload=payload,
                )
                s.add(row)
            else:
                row.session_id = task.session_id
                row.user_id = task.user_id
                row.topic = task.topic[:256]
                row.description = task.description or ""
                row.total_pages = int(task.total_pages)
                row.audience = (task.audience or "")[:128]
                row.global_style = (task.global_style or "")[:128]
                row.status = task.status or "pending"
                row.updated_at = max(now, int(task.last_update or now))
                row.agent_payload = payload
            await s.commit()

    async def load_task(self, task_id: str) -> Optional[TaskState]:
        tid = (task_id or "").strip()
        if not tid:
            return None
        async with self._factory() as s:
            row = await s.get(TaskRow, tid)
            if row is None:
                return None
            return task_from_row_and_payload(
                task_id=row.id,
                session_id=row.session_id,
                user_id=row.user_id,
                topic=row.topic,
                description=row.description or "",
                total_pages=int(row.total_pages or 0),
                audience=row.audience or "",
                global_style=row.global_style or "",
                status=row.status or "pending",
                created_at=row.created_at,
                updated_at=row.updated_at,
                payload_raw=row.agent_payload or "{}",
            )

    async def list_tasks_light(
        self,
        session_id: str,
        page: int,
        page_size: int,
        user_id: Optional[str] = None,
    ) -> tuple[list[TaskListRow], int]:
        """列表接口仅读列字段，不反序列化 agent_payload。"""
        sid = (session_id or "").strip()
        p = max(1, page)
        ps = max(1, min(page_size, 100))
        offset = (p - 1) * ps
        async with self._factory() as s:
            stmt_count = select(func.count()).select_from(TaskRow).where(
                TaskRow.session_id == sid
            )
            if user_id:
                stmt_count = stmt_count.where(TaskRow.user_id == user_id)
            total = int((await s.execute(stmt_count)).scalar_one())

            stmt = (
                select(
                    TaskRow.id,
                    TaskRow.session_id,
                    TaskRow.user_id,
                    TaskRow.topic,
                    TaskRow.status,
                    TaskRow.updated_at,
                )
                .where(TaskRow.session_id == sid)
                .order_by(TaskRow.updated_at.desc())
                .offset(offset)
                .limit(ps)
            )
            if user_id:
                stmt = stmt.where(TaskRow.user_id == user_id)
            res = await s.execute(stmt)
            rows = [
                TaskListRow(
                    task_id=r.id,
                    session_id=r.session_id,
                    user_id=r.user_id,
                    topic=r.topic or "",
                    status=r.status or "pending",
                    updated_at=r.updated_at,
                )
                for r in res.all()
            ]
        return rows, total

    async def save_export(self, exp: ExportState) -> None:
        async with self._factory() as s:
            row = await s.get(ExportRow, exp.export_id)
            if row is None:
                s.add(
                    ExportRow(
                        id=exp.export_id,
                        task_id=exp.task_id,
                        export_format=exp.format[:16],
                        status=exp.status or "pending",
                        download_url=exp.download_url or "",
                        file_size=int(exp.file_size or 0),
                        last_update=int(exp.last_update or utc_ms()),
                    )
                )
            else:
                row.task_id = exp.task_id
                row.export_format = exp.format[:16]
                row.status = exp.status or "pending"
                row.download_url = exp.download_url or ""
                row.file_size = int(exp.file_size or 0)
                row.last_update = int(exp.last_update or utc_ms())
            await s.commit()

    async def load_export(self, export_id: str) -> Optional[ExportState]:
        eid = (export_id or "").strip()
        if not eid:
            return None
        async with self._factory() as s:
            row = await s.get(ExportRow, eid)
            if row is None:
                return None
            return ExportState(
                export_id=row.id,
                task_id=row.task_id,
                format=row.export_format,
                status=row.status or "pending",
                download_url=row.download_url or "",
                file_size=int(row.file_size or 0),
                last_update=row.last_update,
            )
</think>
正在修复 `repository.py`：上一段写入被截断，将补全并实现正确的列表查询。

<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>
Read