#!/usr/bin/env python3
"""
Fun-ASR-Nano WebSocket server.

Protocol matches voice_agent/asr.go:
  1. Client → JSON config  {"mode":"2pass","chunk_size":[5,10,5],"audio_fs":16000,"itn":true,"is_speaking":true}
  2. Client → binary PCM chunks (int16, 16kHz)
  3. Server → {"text":"...","is_final":false,"mode":"2pass-online"}  (streaming partial)
  4. Client → {"is_speaking":false}
  5. Server → {"text":"...","is_final":true,"mode":"2pass-offline"}  (high-quality final)

Usage:
  python3 start_asr.py --model-dir /mnt/d/.../models/asr --port 10096
"""

import asyncio
import argparse
import json
import logging
import os

import numpy as np
import websockets
from funasr import AutoModel

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger(__name__)

MODEL = None

# Send a partial result every ~600ms worth of audio (600ms × 16000Hz × 2 bytes = 19200 bytes)
PARTIAL_THRESHOLD_BYTES = 19200


def load_model(model_dir: str) -> AutoModel:
    model_py = os.path.join(model_dir, "model.py")
    if not os.path.exists(model_py):
        raise FileNotFoundError(
            f"model.py not found in {model_dir}\n"
            "Download it first:\n"
            "  python3 -c \"\n"
            "  from huggingface_hub import hf_hub_download\n"
            "  hf_hub_download('FunAudioLLM/Fun-ASR-Nano-2512', 'model.py',\n"
            f"  local_dir='{model_dir}')\""
        )
    log.info("Loading Fun-ASR-Nano from %s", model_dir)
    return AutoModel(
        model=model_dir,
        trust_remote_code=True,
        remote_code=model_py,
        device="cuda:0",
    )


def run_asr(audio_bytes: bytes, itn: bool) -> str:
    arr = np.frombuffer(audio_bytes, dtype=np.int16).astype(np.float32) / 32768.0
    res = MODEL.generate(input=arr, cache={}, language="zh", itn=itn, batch_size=1)
    return res[0]["text"] if res else ""


async def handle(websocket):
    log.info("client connected: %s", websocket.remote_address)
    buf = bytearray()
    itn = True
    loop = asyncio.get_event_loop()

    try:
        async for message in websocket:
            if isinstance(message, str):
                data = json.loads(message)
                if "mode" in data:
                    itn = data.get("itn", True)
                    log.info("config: mode=%s itn=%s", data.get("mode"), itn)
                elif data.get("is_speaking") is False:
                    if buf:
                        text = await loop.run_in_executor(None, run_asr, bytes(buf), itn)
                        await websocket.send(json.dumps({
                            "text": text, "is_final": True, "mode": "2pass-offline",
                        }))
                        log.info("final: %r", text)
                    buf.clear()
            elif isinstance(message, bytes):
                buf.extend(message)
                if len(buf) >= PARTIAL_THRESHOLD_BYTES:
                    text = await loop.run_in_executor(None, run_asr, bytes(buf), itn)
                    await websocket.send(json.dumps({
                        "text": text, "is_final": False, "mode": "2pass-online",
                    }))
    except websockets.exceptions.ConnectionClosed:
        log.info("client disconnected")
    except Exception:
        log.exception("handler error")


async def main(host: str, port: int):
    async with websockets.serve(handle, host, port, max_size=16 * 1024 * 1024):
        log.info("ASR WebSocket listening on ws://%s:%d", host, port)
        await asyncio.Future()


if __name__ == "__main__":
    p = argparse.ArgumentParser(description="Fun-ASR-Nano WebSocket server")
    p.add_argument("--model-dir", required=True)
    p.add_argument("--host", default="0.0.0.0")
    p.add_argument("--port", type=int, default=10096)
    args = p.parse_args()

    MODEL = load_model(args.model_dir)
    asyncio.run(main(args.host, args.port))
