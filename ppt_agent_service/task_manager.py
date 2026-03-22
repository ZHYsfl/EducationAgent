from __future__ import annotations

import asyncio
import logging
import shutil
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from collections.abc import Coroutine
from typing import Any, Optional
from uuid import uuid4

from ppt_agent_service.db.dto import TaskListRow

logger = logging.getLogger(__name__)


def utc_ms() -> int:
    return int(datetime.now(timezone.utc).timestamp() * 1000)


@dataclass
class SuspendedPageState:
    """§3.9.1 SuspendedPage — 悬挂待人工裁决的页面。"""

    page_id: str
    context_id: str
    reason: str
    question_for_user: str
    suspended_at: int
    last_asked_at: int
    ask_count: int = 0
    pending_feedbacks: list[dict[str, Any]] = field(default_factory=list)


@dataclass
class PageMergeState:
    """§3.9.2 同 bucket 串行合并；链式合并时 V_base 为上一轮输出。"""

    is_running: bool = False
    pending_intents: list[dict[str, Any]] = field(default_factory=list)
    #: 上一轮合并完成后的各页 py_code（page_id -> py_code），作下一轮 V_base
    chain_baseline_pages: dict[str, str] = field(default_factory=dict)
    #: 当前链对应的 VAD base_timestamp；变化时清空 chain_baseline_pages
    chain_vad_timestamp: int = 0


@dataclass
class PageState:
    page_id: str
    slide_index: int
    status: str  # rendering | completed | failed | suspended_for_human
    render_url: str = ""
    py_code: str = ""  # §3.1 PageCode：Python 源码（非裸 HTML）
    version: int = 1
    updated_at: int = 0


@dataclass
class TaskState:
    task_id: str
    user_id: str

    topic: str
    description: str
    total_pages: int
    audience: str
    global_style: str
    session_id: str

    status: str = "pending"  # pending | generating | completed | failed | exporting
    version: int = 1
    last_update: int = 0

    page_order: list[str] = field(default_factory=list)
    current_viewing_page_id: str = ""
    pages: dict[str, PageState] = field(default_factory=dict)

    output_pptx_path: Optional[Path] = None
    running_job: Optional[asyncio.Task] = None

    # Store reference_files / teaching_elements so the generator can use them
    # even on regen (feedback loop).
    reference_files: list[dict] = field(default_factory=list)
    teaching_elements: Optional[dict] = None

    # §3.9.2：生成中收到的反馈先入队，本轮生成结束后再合并进 description 并重跑（不中断当前生成）。
    pending_feedback_lines: list[str] = field(default_factory=list)

    # context_id -> page_id（与 SuspendedPageState.context_id 一致）
    open_conflict_contexts: dict[str, str] = field(default_factory=dict)
    # page_id -> 悬挂状态
    suspended_pages: dict[str, SuspendedPageState] = field(default_factory=dict)
    # page_id 或 "__global__" -> 合并队列状态
    page_merges: dict[str, PageMergeState] = field(default_factory=dict)
    # §3.9.2 同 bucket 串行锁（不进入 dataclass 比较）
    _merge_locks: dict[str, asyncio.Lock] = field(default_factory=dict, repr=False)

    def merge_lock(self, bucket_key: str) -> asyncio.Lock:
        if bucket_key not in self._merge_locks:
            self._merge_locks[bucket_key] = asyncio.Lock()
        return self._merge_locks[bucket_key]

    def bump_version(self) -> None:
        self.version += 1
        self.last_update = utc_ms()


@dataclass
class ExportState:
    export_id: str
    task_id: str
    format: str
    status: str = "pending"  # pending | generating | completed | failed
    download_url: str = ""
    file_size: int = 0
    last_update: int = 0
    running_job: Optional[asyncio.Task] = None


class TaskManager:
    def __init__(self, runs_dir: Path, repo: Optional[Any] = None):
        self.runs_dir = runs_dir
        self.runs_dir.mkdir(parents=True, exist_ok=True)
        self._mu = asyncio.Lock()
        self.tasks: dict[str, TaskState] = {}
        self.exports: dict[str, ExportState] = {}
        # (task_id, page_id) -> asyncio.Task，§3.9.1 45s/3min 时序
        self._suspend_watchers: dict[tuple[str, str], asyncio.Task] = {}
        self._repo: Optional[Any] = repo

    def set_repository(self, repo: Optional[Any]) -> None:
        self._repo = repo

    async def ensure_user_session_db(self, user_id: str, session_id: str) -> None:
        """满足 tasks.session_id / user_id 外键（DB 开启时）。"""
        if self._repo is None:
            return
        uid = (user_id or "").strip()
        sid = (session_id or "").strip()
        if not uid or not sid:
            return
        try:
            await self._repo.ensure_user(uid)
            await self._repo.ensure_session(sid, uid)
        except Exception:
            logger.exception("ensure_user_session_db failed")

    @staticmethod
    def _task_from_list_row(r: TaskListRow) -> TaskState:
        return TaskState(
            task_id=r.task_id,
            user_id=r.user_id,
            topic=r.topic,
            description="",
            total_pages=0,
            audience="",
            global_style="",
            session_id=r.session_id,
            status=r.status,
            last_update=r.updated_at,
        )

    async def persist_task(self, task_id: str) -> None:
        if self._repo is None:
            return
        async with self._mu:
            t = self.tasks.get(task_id)
        if t is None:
            return
        try:
            await self._repo.save_task(t)
        except Exception:
            logger.exception("persist_task(%s) 失败", task_id)

    async def _persist_export_db(self, export_id: str) -> None:
        if self._repo is None:
            return
        async with self._mu:
            exp = self.exports.get(export_id)
        if exp is None:
            return
        try:
            await self._repo.save_export(exp)
        except Exception:
            logger.exception("persist_export(%s) 失败", export_id)

    def new_task_id(self) -> str:
        return f"task_{uuid4()}"

    def new_page_id(self) -> str:
        return f"page_{uuid4()}"

    def new_export_id(self) -> str:
        # §0.4：file_ + UUID v4（标准带横杠形式）
        return f"file_{uuid4()}"

    def task_dir(self, task_id: str) -> Path:
        return self.runs_dir / task_id

    def export_dir(self, task_id: str) -> Path:
        return self.task_dir(task_id) / "exports"

    async def upsert_task(self, state: TaskState) -> None:
        async with self._mu:
            self.tasks[state.task_id] = state
        await self.persist_task(state.task_id)

    async def get_task(self, task_id: str) -> Optional[TaskState]:
        async with self._mu:
            mem = self.tasks.get(task_id)
        if mem is not None:
            return mem
        if self._repo is None:
            return None
        try:
            loaded = await self._repo.load_task(task_id)
        except Exception:
            logger.exception("load_task(%s) 失败", task_id)
            return None
        if loaded is None:
            return None
        async with self._mu:
            existing = self.tasks.get(task_id)
            if existing is not None:
                return existing
            self.tasks[task_id] = loaded
            return loaded

    async def update_task(self, task_id: str, **kwargs) -> None:
        async with self._mu:
            t = self.tasks.get(task_id)
            if not t:
                return
            for k, v in kwargs.items():
                setattr(t, k, v)
        await self.persist_task(task_id)

    async def delete_export(self, export_id: str) -> None:
        async with self._mu:
            if export_id in self.exports:
                del self.exports[export_id]

    async def upsert_export(self, exp: ExportState) -> None:
        async with self._mu:
            self.exports[exp.export_id] = exp
        await self._persist_export_db(exp.export_id)

    async def get_export(self, export_id: str) -> Optional[ExportState]:
        async with self._mu:
            mem = self.exports.get(export_id)
        if mem is not None:
            return mem
        if self._repo is None:
            return None
        try:
            loaded = await self._repo.load_export(export_id)
        except Exception:
            logger.exception("load_export(%s) 失败", export_id)
            return None
        if loaded is None:
            return None
        async with self._mu:
            existing = self.exports.get(export_id)
            if existing is not None:
                return existing
            self.exports[export_id] = loaded
            return loaded

    async def update_export(self, export_id: str, **kwargs) -> None:
        async with self._mu:
            exp = self.exports.get(export_id)
            if not exp:
                return
            for k, v in kwargs.items():
                setattr(exp, k, v)
        await self._persist_export_db(export_id)

    async def append_pending_feedback(self, task_id: str, lines: list[str]) -> None:
        async with self._mu:
            t = self.tasks.get(task_id)
            if not t:
                return
            t.pending_feedback_lines.extend(lines)
        await self.persist_task(task_id)

    async def take_pending_feedback_lines(self, task_id: str) -> list[str]:
        async with self._mu:
            t = self.tasks.get(task_id)
            if not t or not t.pending_feedback_lines:
                return []
            out = t.pending_feedback_lines[:]
            t.pending_feedback_lines.clear()
        await self.persist_task(task_id)
        return out

    async def register_open_conflict(
        self, task_id: str, context_id: str, page_id: str
    ) -> None:
        async with self._mu:
            t = self.tasks.get(task_id)
            if not t:
                return
            t.open_conflict_contexts[context_id] = page_id

    async def clear_open_conflict(self, task_id: str, context_id: str) -> Optional[str]:
        """解除悬挂时返回对应的 page_id（若存在）。"""
        async with self._mu:
            t = self.tasks.get(task_id)
            if not t:
                return None
            return t.open_conflict_contexts.pop(context_id, None)

    def schedule_suspend_watcher(
        self, task_id: str, page_id: str, coro: Coroutine[Any, Any, Any]
    ) -> None:
        """注册悬挂页超时协程（会取消同键旧任务）。"""
        key = (task_id, page_id)
        old = self._suspend_watchers.pop(key, None)
        if old is not None and not old.done():
            old.cancel()
        self._suspend_watchers[key] = asyncio.create_task(coro)

    async def cancel_suspend_watcher(self, task_id: str, page_id: str) -> None:
        key = (task_id, page_id)
        t = self._suspend_watchers.pop(key, None)
        if t is not None and not t.done():
            t.cancel()
            try:
                await t
            except asyncio.CancelledError:
                pass

    async def cancel_all_suspend_watchers(self) -> None:
        for t in list(self._suspend_watchers.values()):
            if not t.done():
                t.cancel()
                try:
                    await t
                except asyncio.CancelledError:
                    pass
        self._suspend_watchers.clear()

    async def clear_task_suspensions_and_conflicts(self, task: TaskState) -> None:
        """整册重生成前清空悬挂与冲突（页 ID 将失效）。"""
        tid = task.task_id
        async with self._mu:
            pids = list(task.suspended_pages.keys())
        for pid in pids:
            await self.cancel_suspend_watcher(tid, pid)
        async with self._mu:
            task.suspended_pages.clear()
            task.open_conflict_contexts.clear()
            task.page_merges.clear()
            task._merge_locks.clear()
        await self.persist_task(tid)

    async def list_tasks_by_session_paged(
        self,
        session_id: str,
        page: int,
        page_size: int,
        user_id: Optional[str] = None,
    ) -> tuple[list[TaskState], int]:
        if self._repo is not None:
            try:
                rows, total = await self._repo.list_tasks_light(
                    session_id, page, page_size, user_id
                )
            except Exception:
                logger.exception("list_tasks_light 失败，回退内存列表")
                rows, total = [], 0
            else:
                out: list[TaskState] = []
                async with self._mu:
                    for r in rows:
                        mem = self.tasks.get(r.task_id)
                        out.append(mem if mem is not None else self._task_from_list_row(r))
                return out, total

        async with self._mu:
            matches = [t for t in self.tasks.values() if t.session_id == session_id]
            if user_id:
                matches = [t for t in matches if t.user_id == user_id]
            matches.sort(key=lambda x: x.last_update, reverse=True)
            total = len(matches)
            p = max(1, page)
            ps = max(1, min(page_size, 100))
            start = (p - 1) * ps
            return matches[start : start + ps], total

