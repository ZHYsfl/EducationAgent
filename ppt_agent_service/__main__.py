"""默认端口 §0.7：PPT Agent 9100。示例: python -m ppt_agent_service"""

from __future__ import annotations

import os

import uvicorn

if __name__ == "__main__":
    uvicorn.run(
        "ppt_agent_service.app:app",
        host=os.getenv("PPT_AGENT_HOST", "0.0.0.0"),
        port=int(os.getenv("PPT_AGENT_PORT", "9100")),
        reload=os.getenv("PPT_AGENT_RELOAD", "").lower() in ("1", "true", "yes"),
    )
