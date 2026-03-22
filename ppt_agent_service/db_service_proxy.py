"""PPT Agent 将 §7.3.7–7.3.10 Sessions 转发至独立 Database Service（无内嵌 DB）。"""
from __future__ import annotations

import os
from typing import Any, Optional

import httpx
from fastapi import Request
from starlette.responses import Response

from ppt_agent_service import error_codes as ec
from ppt_agent_service.api_http_utils import api_error


def _forward_headers(request: Request, *, json_body: bool = False) -> dict[str, str]:
    headers: dict[str, str] = {}
    if json_body:
        headers["Content-Type"] = "application/json"
    for k in ("authorization", "x-internal-key"):
        v = request.headers.get(k)
        if v:
            headers[k] = v
    extra = (os.getenv("PPT_DATABASE_SERVICE_EXTRA_HEADERS") or "").strip()
    if extra:
        for part in extra.split(","):
            if ":" in part:
                hk, hv = part.split(":", 1)
                headers[hk.strip()] = hv.strip()
    return headers


async def proxy_sessions_to_database_service(
    request: Request,
    *,
    json_body: Optional[dict[str, Any]] = None,
) -> Response:
    """
    将当前 Sessions API 请求转发到 `app.state.ppt_remote_db_url`（Database Service）。
    """
    base = getattr(request.app.state, "ppt_remote_db_url", None)
    if not base:
        return api_error(
            ec.EC_DEPENDENCY,
            "未配置 PPT_DATABASE_SERVICE_URL，无法访问 Database Service",
        )
    url = base.rstrip("/") + request.url.path
    qs = str(request.url.query)
    if qs:
        url += "?" + qs
    use_json = json_body is not None
    headers = _forward_headers(request, json_body=use_json)
    timeout = float(os.getenv("PPT_DATABASE_SERVICE_TIMEOUT", "120") or "120")
    async with httpx.AsyncClient(timeout=timeout) as client:
        method = request.method.upper()
        if method == "GET":
            r = await client.get(url, headers=headers)
        elif method == "POST":
            r = await client.post(
                url, headers=headers, json=json_body if json_body is not None else {}
            )
        elif method == "PUT":
            r = await client.put(
                url, headers=headers, json=json_body if json_body is not None else {}
            )
        else:
            r = await client.request(method, url, headers=headers, json=json_body)
    ct = r.headers.get("content-type", "application/json")
    return Response(content=r.content, status_code=r.status_code, media_type=ct)
