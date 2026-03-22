"""
§0.7 Database Service（默认端口 9500）。
- 对外：§7.3.7–7.3.10 /api/v1/sessions
- 对内：/internal/db/* 供 PPT Agent（HTTPPPTRepository）持久化 tasks/exports
"""
from __future__ import annotations

import logging
import os
from contextlib import asynccontextmanager
from typing import Any, Optional

from fastapi import FastAPI, Request
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException

from ppt_agent_service import error_codes as ec
from ppt_agent_service.api_http_utils import (
    api_error,
    assert_body_user_matches_jwt,
    assert_session_owner,
)
from ppt_agent_service.auth_middleware import PPTAuthMiddleware
from ppt_agent_service.db.engine import close_db, init_db
from ppt_agent_service.db.repository import PPTRepository
from ppt_agent_service.db.wire_format import (
    export_from_persist_wire,
    export_to_persist_wire,
    task_from_persist_wire,
    task_to_persist_wire,
)
from ppt_agent_service.schemas import (
    APIResponse,
    CreateSessionRequest,
    CreateSessionResponseData,
    SessionDetailData,
    SessionListData,
    SessionListItem,
    UpdateSessionRequest,
)

logger = logging.getLogger(__name__)

ALLOWED_SESSION_STATUSES = frozenset({"active", "completed", "archived"})


def verify_user_id_allowed(user_id: str) -> Optional[str]:
    mode = os.getenv("PPT_USER_VERIFY", "off").lower().strip()
    if mode != "allowlist":
        return None
    raw = os.getenv("PPT_USER_ALLOWLIST", "")
    allow = {x.strip() for x in raw.split(",") if x.strip()}
    if not allow:
        return None
    if user_id not in allow:
        return "user_id 不存在"
    return None


@asynccontextmanager
async def _lifespan(app: FastAPI):
    fac = None
    try:
        fac = await init_db()
    except Exception:
        logger.exception("Database Service 初始化失败")
    app.state.repo = PPTRepository(fac) if fac is not None else None
    yield
    await close_db()
    app.state.repo = None


app = FastAPI(
    title="EducationAgent Database Service",
    lifespan=_lifespan,
)
app.add_middleware(PPTAuthMiddleware)


@app.exception_handler(RequestValidationError)
async def _validation_handler(request: Request, exc: RequestValidationError):
    errs = exc.errors()
    parts: list[str] = []
    for e in errs[:8]:
        loc = ".".join(str(x) for x in e.get("loc", ()))
        parts.append(f"{loc}: {e.get('msg', '')}")
    msg = "请求参数无效：" + ("; ".join(parts) if parts else str(exc))
    return api_error(ec.EC_PARAM, msg[:800])


@app.exception_handler(StarletteHTTPException)
async def _http_exception_handler(request: Request, exc: StarletteHTTPException):
    if exc.status_code == 404:
        return api_error(ec.EC_NOT_FOUND, str(exc.detail) or "资源不存在")
    return api_error(ec.EC_INTERNAL, str(exc.detail) or "请求处理失败")


def _repo(request: Request) -> Optional[PPTRepository]:
    return getattr(request.app.state, "repo", None)


def _require_repo(request: Request) -> Optional[JSONResponse]:
    if _repo(request) is None:
        return api_error(
            ec.EC_DEPENDENCY,
            "数据库未启用或初始化失败（PPT_DATABASE_ENABLED / PPT_DATABASE_URL）",
        )
    return None


# ── §7.3.7–7.3.10 对外 Sessions ─────────────────────────────


@app.post("/api/v1/sessions")
async def create_session(req: CreateSessionRequest, request: Request):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    uid = (req.user_id or "").strip()
    if not uid:
        return api_error(ec.EC_PARAM, "参数 user_id 不能为空")
    mismatch = assert_body_user_matches_jwt(request, uid)
    if mismatch is not None:
        return mismatch
    uerr = verify_user_id_allowed(uid)
    if uerr:
        return api_error(ec.EC_NOT_FOUND, uerr)
    repo = _repo(request)
    assert repo is not None
    sid = await repo.create_session(uid, (req.title or "").strip())
    return JSONResponse(
        status_code=200,
        content=APIResponse(
            code=200,
            message="success",
            data=CreateSessionResponseData(session_id=sid).model_dump(),
        ).model_dump(),
    )


@app.get("/api/v1/sessions/{session_id}")
async def get_session(session_id: str, request: Request):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    sid = (session_id or "").strip()
    if not sid:
        return api_error(ec.EC_PARAM, "session_id 无效")
    repo = _repo(request)
    assert repo is not None
    row = await repo.get_session(sid)
    if row is None:
        return api_error(ec.EC_NOT_FOUND, "session_id 不存在")
    denied = assert_session_owner(request, row.user_id)
    if denied is not None:
        return denied
    data = SessionDetailData(
        session_id=row.session_id,
        user_id=row.user_id,
        title=row.title,
        status=row.status,
        created_at=row.created_at,
        updated_at=row.updated_at,
    )
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=data.model_dump()).model_dump(),
    )


@app.get("/api/v1/sessions")
async def list_sessions(
    request: Request,
    user_id: str,
    page: int = 1,
    page_size: int = 20,
):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    uid = (user_id or "").strip()
    if not uid:
        return api_error(ec.EC_PARAM, "缺少或无效的查询参数 user_id")
    mismatch = assert_body_user_matches_jwt(request, uid)
    if mismatch is not None:
        return mismatch
    repo = _repo(request)
    assert repo is not None
    rows, total = await repo.list_sessions_paged(uid, page, page_size)
    p = max(1, page)
    ps = max(1, min(page_size, 100))
    items = [
        SessionListItem(
            session_id=r.session_id,
            user_id=r.user_id,
            title=r.title,
            status=r.status,
            created_at=r.created_at,
            updated_at=r.updated_at,
        )
        for r in rows
    ]
    data = SessionListData(total=total, page=p, page_size=ps, items=items).model_dump()
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=data).model_dump(),
    )


@app.put("/api/v1/sessions/{session_id}")
async def update_session(
    session_id: str, body: UpdateSessionRequest, request: Request
):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    sid = (session_id or "").strip()
    if not sid:
        return api_error(ec.EC_PARAM, "session_id 无效")
    if body.title is None and body.status is None:
        return api_error(ec.EC_PARAM, "至少需要提供 title 或 status 之一")
    if body.status is not None:
        st = body.status.strip()
        if st not in ALLOWED_SESSION_STATUSES:
            return api_error(
                ec.EC_PARAM,
                f"非法 status，允许：{', '.join(sorted(ALLOWED_SESSION_STATUSES))}",
            )
    repo = _repo(request)
    assert repo is not None
    row = await repo.get_session(sid)
    if row is None:
        return api_error(ec.EC_NOT_FOUND, "session_id 不存在")
    denied = assert_session_owner(request, row.user_id)
    if denied is not None:
        return denied
    title_val = body.title.strip() if body.title is not None else None
    status_val = body.status.strip() if body.status is not None else None
    ok = await repo.update_session(sid, title=title_val, status=status_val)
    if not ok:
        return api_error(ec.EC_NOT_FOUND, "session_id 不存在")
    row2 = await repo.get_session(sid)
    assert row2 is not None
    data = SessionDetailData(
        session_id=row2.session_id,
        user_id=row2.user_id,
        title=row2.title,
        status=row2.status,
        created_at=row2.created_at,
        updated_at=row2.updated_at,
    )
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=200, message="success", data=data.model_dump()).model_dump(),
    )


# ── /internal/db：模块间持久化（须 X-Internal-Key 或 JWT）────────────────


@app.post("/internal/db/ensure-user")
async def internal_ensure_user(request: Request, body: dict[str, Any]):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    await _repo(request).ensure_user(
        str(body.get("user_id") or ""),
        display_name=str(body.get("display_name") or ""),
    )
    return {"ok": True}


@app.post("/internal/db/ensure-session")
async def internal_ensure_session(request: Request, body: dict[str, Any]):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    await _repo(request).ensure_session(
        str(body.get("session_id") or ""),
        str(body.get("user_id") or ""),
        title=str(body.get("title") or ""),
    )
    return {"ok": True}


@app.post("/internal/db/create-session")
async def internal_create_session(request: Request, body: dict[str, Any]):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    uid = str(body.get("user_id") or "").strip()
    if not uid:
        return api_error(ec.EC_PARAM, "user_id 必填")
    sid = await _repo(request).create_session(uid, str(body.get("title") or ""))
    return {"session_id": sid}


@app.get("/internal/db/sessions/{session_id}")
async def internal_get_session(session_id: str, request: Request):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    row = await _repo(request).get_session(session_id.strip())
    if row is None:
        raise StarletteHTTPException(status_code=404, detail="session not found")
    return {
        "session_id": row.session_id,
        "user_id": row.user_id,
        "title": row.title,
        "status": row.status,
        "created_at": row.created_at,
        "updated_at": row.updated_at,
    }


@app.get("/internal/db/sessions")
async def internal_list_sessions(
    request: Request,
    user_id: str,
    page: int = 1,
    page_size: int = 20,
):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    rows, total = await _repo(request).list_sessions_paged(user_id, page, page_size)
    items = [
        {
            "session_id": r.session_id,
            "user_id": r.user_id,
            "title": r.title,
            "status": r.status,
            "created_at": r.created_at,
            "updated_at": r.updated_at,
        }
        for r in rows
    ]
    return {"items": items, "total": total}


@app.patch("/internal/db/sessions/{session_id}")
async def internal_patch_session(session_id: str, request: Request, body: dict[str, Any]):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    kw: dict[str, Any] = {}
    if "title" in body:
        v = body["title"]
        kw["title"] = v.strip() if isinstance(v, str) else None
    if "status" in body:
        v = body["status"]
        kw["status"] = v.strip() if isinstance(v, str) else None
    if not kw:
        return {"ok": True}
    ok = await _repo(request).update_session(session_id.strip(), **kw)
    if not ok:
        raise StarletteHTTPException(status_code=404, detail="session not found")
    return {"ok": True}


@app.put("/internal/db/tasks/{task_id}")
async def internal_put_task(task_id: str, request: Request, body: dict[str, Any]):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    body = dict(body)
    body["id"] = task_id.strip()
    task = task_from_persist_wire(body)
    await _repo(request).save_task(task)
    return {"ok": True}


@app.get("/internal/db/tasks/{task_id}")
async def internal_get_task(task_id: str, request: Request):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    task = await _repo(request).load_task(task_id.strip())
    if task is None:
        raise StarletteHTTPException(status_code=404, detail="task not found")
    return task_to_persist_wire(task)


@app.get("/internal/db/task-list")
async def internal_task_list(
    request: Request,
    session_id: str,
    page: int = 1,
    page_size: int = 20,
    user_id: Optional[str] = None,
):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    rows, total = await _repo(request).list_tasks_light(
        session_id, page, page_size, user_id
    )
    items = [
        {
            "task_id": r.task_id,
            "session_id": r.session_id,
            "user_id": r.user_id,
            "topic": r.topic,
            "status": r.status,
            "updated_at": r.updated_at,
        }
        for r in rows
    ]
    return {"items": items, "total": total}


@app.put("/internal/db/exports/{export_id}")
async def internal_put_export(export_id: str, request: Request, body: dict[str, Any]):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    body = dict(body)
    body["export_id"] = export_id.strip()
    exp = export_from_persist_wire(body)
    await _repo(request).save_export(exp)
    return {"ok": True}


@app.get("/internal/db/exports/{export_id}")
async def internal_get_export(export_id: str, request: Request):
    dep = _require_repo(request)
    if dep is not None:
        return dep
    exp = await _repo(request).load_export(export_id.strip())
    if exp is None:
        raise StarletteHTTPException(status_code=404, detail="export not found")
    return export_to_persist_wire(exp)


@app.get("/healthz")
async def healthz():
    return {"status": "ok", "service": "database_service"}


@app.get("/")
async def root():
    return {"service": "database_service", "port_hint": 9500}
