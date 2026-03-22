from __future__ import annotations

import logging
import os
from pathlib import Path
from typing import Optional

from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)

from ppt_agent_service.db.models import Base

logger = logging.getLogger(__name__)

_engine: Optional[AsyncEngine] = None
_session_factory: Optional[async_sessionmaker[AsyncSession]] = None


def database_url() -> str:
    u = (os.getenv("PPT_DATABASE_URL") or "").strip()
    if u:
        return u
    repo_root = Path(__file__).resolve().parents[2]
    db_path = repo_root / "runs" / "ppt_agent.db"
    db_path.parent.mkdir(parents=True, exist_ok=True)
    return f"sqlite+aiosqlite:///{db_path.as_posix()}"


def is_db_enabled() -> bool:
    return os.getenv("PPT_DATABASE_ENABLED", "true").lower() in (
        "1",
        "true",
        "yes",
    )


async def init_db() -> Optional[async_sessionmaker[AsyncSession]]:
    global _engine, _session_factory
    if not is_db_enabled():
        logger.info("PPT_DATABASE_ENABLED=false，跳过 DB 初始化")
        return None
    url = database_url()
    _engine = create_async_engine(
        url,
        echo=os.getenv("PPT_DATABASE_ECHO", "").lower() in ("1", "true", "yes"),
    )
    if url.startswith("sqlite"):
        from sqlalchemy import event

        @event.listens_for(_engine.sync_engine, "connect")
        def _fk(dbapi_connection, connection_record):  # type: ignore[no-untyped-def]
            cursor = dbapi_connection.cursor()
            cursor.execute("PRAGMA foreign_keys=ON")
            cursor.close()

    async with _engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)

    _session_factory = async_sessionmaker(
        _engine, expire_on_commit=False, class_=AsyncSession
    )
    logger.info("PPT Agent DB 已初始化: %s", url.split("@")[-1] if "@" in url else url)
    return _session_factory


async def close_db() -> None:
    global _engine, _session_factory
    if _engine is not None:
        await _engine.dispose()
    _engine = None
    _session_factory = None


def get_session_factory() -> Optional[async_sessionmaker[AsyncSession]]:
    return _session_factory
