"""
§0.3 鉴权：外部 JWT（Authorization: Bearer）或模块间 X-Internal-Key。
"""
from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Optional

import jwt
from jwt import ExpiredSignatureError, InvalidTokenError

from ppt_agent_service import error_codes as ec

# 与 Voice 调用本服务/本服务调用 Voice 共用时可配置
INTERNAL_KEY = os.getenv("INTERNAL_KEY", "").strip()
JWT_SECRET = os.getenv("PPT_JWT_SECRET", "").strip()
JWT_ALG = os.getenv("PPT_JWT_ALGORITHM", "HS256").strip() or "HS256"
# 本地开发可设 PPT_AUTH_DISABLED=true 关闭鉴权（不推荐生产）
AUTH_DISABLED = os.getenv("PPT_AUTH_DISABLED", "").lower() in ("1", "true", "yes")
JWT_REQUIRE_EXP = os.getenv("PPT_JWT_REQUIRE_EXP", "true").lower() in (
    "1",
    "true",
    "yes",
    "",
)


@dataclass
class AuthContext:
    """请求鉴权结果；is_internal 表示通过 INTERNAL_KEY，可代操任意 task。"""

    user_id: str = ""
    username: str = ""
    is_internal: bool = False


def is_auth_enforced() -> bool:
    if AUTH_DISABLED:
        return False
    # 至少配置 JWT 密钥或内部密钥之一则启用鉴权门槛
    return bool(JWT_SECRET or INTERNAL_KEY)


def authenticate_bearer_or_internal(
    authorization: Optional[str],
    x_internal_key: Optional[str],
) -> tuple[Optional[AuthContext], Optional[int], Optional[str]]:
    """
    返回 (context, error_code, error_message)。
    成功时 error_code/message 为 None。
    """
    if not is_auth_enforced():
        return AuthContext(is_internal=True), None, None

    if INTERNAL_KEY and (x_internal_key or "").strip() == INTERNAL_KEY:
        return AuthContext(is_internal=True), None, None

    if not JWT_SECRET:
        return (
            None,
            ec.EC_AUTH_REQUIRED,
            "缺少有效认证：请配置 PPT_JWT_SECRET 并携带 Authorization: Bearer <token>，"
            "或配置 INTERNAL_KEY 并携带 X-Internal-Key",
        )

    auth = authorization or ""
    if not auth[:7].lower().startswith("bearer "):
        return (
            None,
            ec.EC_AUTH_REQUIRED,
            "缺少认证：应携带 Authorization: Bearer <token>，"
            "或有效的 X-Internal-Key（模块间调用）",
        )

    token = auth[7:].strip()
    if not token:
        return None, ec.EC_AUTH_REQUIRED, "Bearer token 为空"

    decode_opts: dict = {"verify_signature": True}
    if JWT_REQUIRE_EXP:
        decode_opts["require"] = ["exp"]

    try:
        payload = jwt.decode(
            token,
            JWT_SECRET,
            algorithms=[JWT_ALG],
            options=decode_opts,
        )
    except ExpiredSignatureError:
        return None, ec.EC_AUTH_EXPIRED, "Token 已过期"
    except InvalidTokenError:
        return None, ec.EC_AUTH_INVALID, "Token 无效"

    if not isinstance(payload, dict):
        return None, ec.EC_AUTH_INVALID, "Token 载荷无效"

    uid = str(payload.get("user_id") or payload.get("sub") or "").strip()
    if not uid:
        return None, ec.EC_AUTH_INVALID, "Token 缺少 user_id（或 sub）"

    username = str(payload.get("username") or "").strip()
    return AuthContext(user_id=uid, username=username, is_internal=False), None, None
