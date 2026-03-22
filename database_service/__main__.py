"""python -m database_service"""

from __future__ import annotations

import os

import uvicorn

if __name__ == "__main__":
    uvicorn.run(
        "database_service.app:app",
        host=os.getenv("DATABASE_SERVICE_HOST", "0.0.0.0"),
        port=int(os.getenv("DATABASE_SERVICE_PORT", "9500")),
        reload=os.getenv("DATABASE_SERVICE_RELOAD", "").lower() in ("1", "true", "yes"),
    )
