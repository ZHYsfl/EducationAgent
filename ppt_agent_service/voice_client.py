"""
§3.4 PPT Agent → Voice Agent：ppt_message / ppt_message_tool 别名。
"""
from __future__ import annotations

import json
import os
from typing import Any

import httpx

VOICE_AGENT_BASE_URL = os.getenv("VOICE_AGENT_BASE_URL", "http://localhost:9000")
INTERNAL_KEY = os.getenv("INTERNAL_KEY", "")


def _voice_json_accepted(body: str) -> bool:
    """§3.4：仅 HTTP 成功不够；需 JSON 且 code==200 或 message==accepted。"""
    try:
        obj = json.loads(body)
    except json.JSONDecodeError:
        return False
    if not isinstance(obj, dict):
        return False
    if obj.get("code") == 200:
        return True
    msg = obj.get("message")
    if isinstance(msg, str) and msg.strip().lower() == "accepted":
        return True
    return False


async def voice_ppt_message(
    *,
    task_id: str,
    page_id: str = "",
    priority: str = "normal",
    context_id: str = "",
    tts_text: str = "",
    msg_type: str = "ppt_status",
    timeout: float = 10.0,
) -> None:
    """
    msg_type: conflict_question | ppt_status
    priority: high（冲突求助）| normal
    """
    headers: dict[str, str] = {}
    if INTERNAL_KEY:
        headers["X-Internal-Key"] = INTERNAL_KEY

    payload: dict[str, Any] = {
        "task_id": task_id,
        "page_id": page_id,
        "priority": priority,
        "context_id": context_id,
        "tts_text": tts_text,
        "msg_type": msg_type,
    }

    urls = [
        f"{VOICE_AGENT_BASE_URL.rstrip('/')}/api/v1/voice/ppt_message",
        f"{VOICE_AGENT_BASE_URL.rstrip('/')}/api/v1/voice/ppt_message_tool",
    ]

    last_err: Exception | None = None
    try:
        async with httpx.AsyncClient(timeout=timeout) as client:
            for url in urls:
                try:
                    r = await client.post(url, json=payload, headers=headers)
                    if r.status_code == 200 and _voice_json_accepted(r.text):
                        return
                except Exception as e:
                    last_err = e
                    continue
    except Exception as e:
        last_err = e
    if last_err is not None:
        # 尽力而为，不抛给调用方
        return
