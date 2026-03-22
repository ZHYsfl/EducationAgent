"""
§0.3：对 /api/v1/* 校验 JWT 或 X-Internal-Key（healthz、OpenAPI、静态资源除外）。
"""
from __future__ import annotations

from starlette.middleware.base import BaseHTTPMiddleware
from starlette.requests import Request
from starlette.responses import JSONResponse

from ppt_agent_service.auth import AuthContext, authenticate_bearer_or_internal, is_auth_enforced


def _skip_auth(path: str) -> bool:
    if path == "/healthz":
        return True
    if path.startswith("/docs") or path.startswith("/redoc"):
        return True
    if path in ("/openapi.json", "/favicon.ico"):
        return True
    # /internal/* 须走鉴权（模块间 X-Internal-Key 或 JWT），不可跳过
    if path.startswith("/internal/"):
        return False
    # 仅保护业务 API；/api/v1 以外（若有）不强制
    if not path.startswith("/api/v1/"):
        return True
    return False


class PPTAuthMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request: Request, call_next):
        path = request.url.path
        if _skip_auth(path):
            if not is_auth_enforced():
                request.state.auth = AuthContext(is_internal=True)
            else:
                # 未走 /api/v1 的路径：不强制，但便于下游统一取 ctx
                request.state.auth = AuthContext(is_internal=True)
            return await call_next(request)

        auth_h = request.headers.get("authorization")
        xik = request.headers.get("x-internal-key") or request.headers.get("X-Internal-Key")

        ctx, code, msg = authenticate_bearer_or_internal(auth_h, xik)
        if code is not None:
            return JSONResponse(
                status_code=200,
                content={"code": code, "message": msg or "认证失败", "data": None},
            )

        request.state.auth = ctx
        return await call_next(request)
