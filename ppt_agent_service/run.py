from __future__ import annotations

import os

import uvicorn


def main() -> None:
    port = int(os.getenv("PPTAGENT_HTTP_PORT", "9100"))
    uvicorn.run("ppt_agent_service.app:app", host="0.0.0.0", port=port, reload=False)


if __name__ == "__main__":
    main()

