"""跨服务共用的 JSON 业务响应与 JWT 会话校验（避免 database_service 依赖 app 模块）。"""
from __future__ import annotations

from typing import Optional

from fastapi import Request
from fastapi.responses import JSONResponse

from ppt_agent_service import error_codes as ec
from ppt_agent_service.auth import AuthContext, is_auth_enforced
from ppt_agent_service.schemas import APIResponse


def api_error(code: int, message: str) -> JSONResponse:
    return JSONResponse(
        status_code=200,
        content=APIResponse(code=code, message=message, data=None).model_dump(),
    )


def assert_body_user_matches_jwt(
    request: Request, body_user_id: str
) -> Optional[JSONResponse]:
    """§0.3：JWT 场景下 body.user_id 须与 token 一致（模块内部密钥调用跳过）。"""
    if not is_auth_enforced():
        return None
    ctx = getattr(request.state, "auth", None)
    if not isinstance(ctx, AuthContext) or ctx.is_internal:
        return None
    uid = (body_user_id or "").strip()
    if not uid:
        return None
    if uid != ctx.user_id.strip():
        return api_error(ec.EC_AUTH_USER_MISMATCH, "请求中的 user_id 与 JWT 不一致")
    return None


def assert_session_owner(request: Request, owner_user_id: str) -> Optional[JSONResponse]:
    """JWT 用户仅能操作本人会话；内部密钥放行。"""
    if not is_auth_enforced():
        return None
    ctx = getattr(request.state, "auth", None)
    if not isinstance(ctx, AuthContext) or ctx.is_internal:
        return None
    if not ctx.user_id or ctx.user_id.strip() != (owner_user_id or "").strip():
        return api_error(ec.EC_AUTH_FORBIDDEN_TASK, "无权访问该会话")
    return None
