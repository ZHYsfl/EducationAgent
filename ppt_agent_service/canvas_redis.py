"""
§3.1 画布与 VAD：Redis 存当前画布；VAD 时将画布深拷贝为 snapshot:{task_id}:{timestamp}，TTL=300s。
"""
from __future__ import annotations

import copy
import json
import os
from dataclasses import dataclass
from typing import Any, Optional

from ppt_agent_service.task_manager import TaskState, utc_ms

REDIS_URL = os.getenv("REDIS_URL", "redis://127.0.0.1:6379/0")
SNAPSHOT_TTL_SEC = int(os.getenv("PPT_SNAPSHOT_TTL_SEC", "300"))


def canvas_key(task_id: str) -> str:
    return f"canvas:{task_id}"


def snapshot_key(task_id: str, timestamp: int) -> str:
    return f"snapshot:{task_id}:{timestamp}"


def task_to_canvas_document(
    task: TaskState,
    *,
    page_status_override: Optional[str] = None,
    ts: Optional[int] = None,
) -> dict[str, Any]:
    """
    §3.1.2：`pages[pid]` 仅含 PageCode 三字段；`render_url`/`last_update` 放在 `page_display`。
    """
    pages: dict[str, Any] = {}
    page_display: dict[str, Any] = {}
    for pid, p in task.pages.items():
        pages[pid] = {
            "page_id": p.page_id,
            "py_code": p.py_code,
            "status": page_status_override or p.status,
        }
        page_display[pid] = {
            "render_url": p.render_url,
            "last_update": p.updated_at,
        }
    return {
        "task_id": task.task_id,
        "timestamp": ts if ts is not None else utc_ms(),
        "page_order": list(task.page_order),
        "current_viewing_page_id": task.current_viewing_page_id or "",
        "pages": pages,
        "page_display": page_display,
    }


def to_strict_snapshot_payload(doc: dict[str, Any]) -> dict[str, Any]:
    """写入 snapshot: 时 pages 仅保留 page_id/py_code/status，去掉 page_display。"""
    snap = copy.deepcopy(doc)
    snap.pop("page_display", None)
    pages = snap.get("pages")
    if isinstance(pages, dict):
        for k, v in list(pages.items()):
            if isinstance(v, dict):
                pages[k] = {
                    "page_id": v.get("page_id", k),
                    "py_code": v.get("py_code", ""),
                    "status": v.get("status", "completed"),
                }
    return snap


def get_py_code_from_canvas_doc(doc: dict[str, Any], page_id: str) -> str:
    ent = (doc.get("pages") or {}).get(page_id)
    if isinstance(ent, dict):
        return str(ent.get("py_code", "") or "")
    return ""


@dataclass
class CanvasRedis:
    """异步 Redis 客户端；连接失败时 enabled=False，读接口由调用方回退内存。"""

    url: str = REDIS_URL
    _client: Any = None
    enabled: bool = False

    async def connect(self) -> None:
        try:
            import redis.asyncio as aioredis

            self._client = aioredis.from_url(
                self.url,
                decode_responses=True,
                socket_connect_timeout=2.0,
            )
            await self._client.ping()
            self.enabled = True
        except Exception:
            self._client = None
            self.enabled = False

    async def close(self) -> None:
        if self._client is not None:
            try:
                await self._client.aclose()
            except Exception:
                pass
        self._client = None
        self.enabled = False

    async def ping(self) -> bool:
        if not self.enabled or not self._client:
            return False
        try:
            return bool(await self._client.ping())
        except Exception:
            return False

    async def redis_unavailable_reason(self) -> Optional[str]:
        """§0.2 / 502xx：Redis 不可用时的人类可读原因（无则返回 None）。"""
        if not self.enabled or not self._client:
            return "Redis 未启用或未连接（请检查 REDIS_URL）"
        try:
            if not await self._client.ping():
                return "Redis 无响应"
        except Exception as e:
            return str(e) or "Redis 连接异常"
        return None

    async def save_canvas_document(self, task_id: str, doc: dict[str, Any]) -> None:
        if not self.enabled or not self._client:
            return
        await self._client.set(canvas_key(task_id), json.dumps(doc, ensure_ascii=False))

    async def load_canvas_document(self, task_id: str) -> Optional[dict[str, Any]]:
        if not self.enabled or not self._client:
            return None
        raw = await self._client.get(canvas_key(task_id))
        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return None

    async def load_snapshot_document(
        self, task_id: str, timestamp: int
    ) -> Optional[dict[str, Any]]:
        """读取 VAD 快照，供三路合并 system_patch / V_base 对齐。"""
        if not self.enabled or not self._client:
            return None
        raw = await self._client.get(snapshot_key(task_id, timestamp))
        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return None

    async def save_canvas_from_task(
        self,
        task: TaskState,
        *,
        page_status_override: Optional[str] = None,
    ) -> None:
        doc = task_to_canvas_document(
            task, page_status_override=page_status_override, ts=utc_ms()
        )
        await self.save_canvas_document(task.task_id, doc)

    async def vad_deep_copy_snapshot(
        self,
        task_id: str,
        vad_timestamp: int,
        viewing_page_id: str,
        live_doc: dict[str, Any],
    ) -> str:
        """
        将 live_doc 深拷贝写入 snapshot:{task_id}:{vad_timestamp}，TTL=SNAPSHOT_TTL_SEC。
        在副本中写入 vad_viewing_page_id 便于与 PPTFeedbackRequest.base_timestamp 对齐。
        """
        if not self.enabled or not self._client:
            raise RuntimeError("Redis 不可用")
        snap = to_strict_snapshot_payload(copy.deepcopy(live_doc))
        snap["timestamp"] = vad_timestamp
        snap["vad_viewing_page_id"] = viewing_page_id or ""
        key = snapshot_key(task_id, vad_timestamp)
        await self._client.set(
            key,
            json.dumps(snap, ensure_ascii=False),
            ex=SNAPSHOT_TTL_SEC,
        )
        return key


canvas_store = CanvasRedis()
