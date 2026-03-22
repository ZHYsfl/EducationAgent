#!/usr/bin/env python3
"""
CosyVoice3 streaming TTS server.

Exposes POST /inference_sft compatible with voice_agent/tts.go.
Uses zero-shot voice cloning with a reference WAV file.

Usage:
  COSYVOICE_REPO=~/voice-services/CosyVoice \
  REF_WAV=~/voice-services/CosyVoice/asset/zero_shot_prompt.wav \
  python3 start_tts.py --model-dir /mnt/d/.../models/tts --port 50000
"""

import argparse
import io
import logging
import os
import sys

import numpy as np
import soundfile as sf
from fastapi import FastAPI, Form
from fastapi.responses import StreamingResponse
import uvicorn

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger(__name__)

app = FastAPI()

cosyvoice_model = None
REF_WAV: str = ""
SAMPLE_RATE = 24000

# Must match the reference WAV content
PROMPT_TEXT = "You are a helpful assistant.<|endofprompt|>希望你以后能够做的比我还好呦。"


@app.post("/inference_sft")
async def inference_sft(
    tts_text: str = Form(...),
    mode: str = Form("sft"),
    stream: str = Form("1"),
):
    def audio_stream():
        first = True
        for chunk in cosyvoice_model.inference_zero_shot(
            tts_text, PROMPT_TEXT, REF_WAV, stream=True
        ):
            audio = chunk["tts_speech"].numpy()
            if audio.ndim > 1:
                audio = audio.squeeze(0)
            pcm = (audio * 32768.0).clip(-32768, 32767).astype(np.int16)

            if first:
                # First chunk: include WAV header so browser can decode
                buf = io.BytesIO()
                sf.write(buf, pcm, SAMPLE_RATE, format="WAV", subtype="PCM_16")
                buf.seek(0)
                yield buf.read()
                first = False
            else:
                yield pcm.tobytes()

    return StreamingResponse(audio_stream(), media_type="audio/wav")


if __name__ == "__main__":
    p = argparse.ArgumentParser(description="CosyVoice3 TTS FastAPI server")
    p.add_argument("--model-dir", required=True)
    p.add_argument("--ref-wav", default=os.environ.get("REF_WAV", ""))
    p.add_argument("--host", default="0.0.0.0")
    p.add_argument("--port", type=int, default=50000)
    args = p.parse_args()

    if not args.ref_wav:
        p.error("--ref-wav is required (or set REF_WAV env var)")

    cosyvoice_repo = os.environ.get("COSYVOICE_REPO", "")
    if cosyvoice_repo:
        sys.path.insert(0, cosyvoice_repo)
        sys.path.insert(0, os.path.join(cosyvoice_repo, "third_party/Matcha-TTS"))
    else:
        log.warning("COSYVOICE_REPO not set; assuming cosyvoice is already installed")

    from cosyvoice.cli.cosyvoice import AutoModel  # noqa: E402

    REF_WAV = args.ref_wav
    log.info("Loading CosyVoice3 from %s", args.model_dir)
    cosyvoice_model = AutoModel(model_dir=args.model_dir)
    log.info("Model loaded. Starting TTS server on %s:%d", args.host, args.port)
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")
