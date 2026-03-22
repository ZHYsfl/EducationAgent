#!/usr/bin/env python3
"""
Qwen3-ASR WebSocket 服务 - vLLM 后端
比 Fun-ASR-Nano 更轻量，效果更好

依赖安装:
    pip install -U qwen-asr[vllm]
    
Usage:
    python3 start_asr_vllm.py --model Qwen/Qwen3-ASR-0.6B --port 10096
    
    # 或使用本地模型路径
    python3 start_asr_vllm.py --model /mnt/d/.../models/asr --port 10096
"""

import asyncio
import argparse
import json
import logging
import sys
from typing import Optional

import numpy as np
import torch
import websockets
from qwen_asr import Qwen3ASRModel

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
log = logging.getLogger(__name__)

MODEL = None

# 每 600ms 音频发送一次中间结果 (16000Hz * 0.6s * 2 bytes = 19200 bytes)
PARTIAL_THRESHOLD_BYTES = 19200


def load_model(
    model_path: str,
    gpu_util: float = 0.75,
    max_model_len: int = 1024,
    backend: str = "auto",
):
    """加载 Qwen3-ASR 模型，优先 vLLM，失败时可回退 transformers。"""
    log.info(f"Loading Qwen3-ASR from {model_path}")

    use_vllm = backend in ("auto", "vllm")
    use_tf_fallback = backend == "auto"

    if use_vllm:
        try:
            # 使用 vLLM 后端启动
            model = Qwen3ASRModel.LLM(
                model=model_path,
                gpu_memory_utilization=gpu_util,
                max_model_len=max_model_len,
                max_inference_batch_size=1,  # WebSocket 通常单流
                max_new_tokens=256,
            )
            setattr(model, "_runtime_backend", "vllm")
            log.info("ASR backend: vllm")
            return model
        except Exception as e:
            msg = str(e)
            incompatible_msg = "build_data_parser" in msg and "v0.16" in msg
            no_kv_cache_msg = "No available memory for the cache blocks" in msg

            if no_kv_cache_msg:
                raise RuntimeError(
                    "GPU 显存不足，无法分配 KV cache。\n"
                    "请重试并提高 --gpu-util、降低 --max-model-len，例如：\n"
                    "  --gpu-util 0.85 --max-model-len 512\n"
                    "同时确保先关闭其他占显存进程。"
                ) from e

            if incompatible_msg and not use_tf_fallback:
                raise RuntimeError(
                    "vLLM 与 qwen-asr 版本不兼容（仅 vllm 模式下不回退）。\n"
                    "可改为 --backend auto 或 --backend transformers，\n"
                    "或在独立环境安装匹配版本：pip install -U 'qwen-asr[vllm]'。"
                ) from e

            if not use_tf_fallback:
                raise

            log.warning("vLLM backend failed, fallback to transformers backend: %s", msg)

    # transformers 回退路径（兼容性更强，但吞吐低于 vLLM）
    model = Qwen3ASRModel.from_pretrained(
        model_path,
        dtype=torch.bfloat16,
        device_map="cuda:0",
        max_inference_batch_size=1,
        max_new_tokens=256,
    )
    setattr(model, "_runtime_backend", "transformers")
    log.info("ASR backend: transformers (fallback)")
    return model


def run_asr(audio_bytes: bytes, language: Optional[str] = None) -> str:
    """执行 ASR 识别"""
    # 将 PCM bytes 转为 numpy array
    arr = np.frombuffer(audio_bytes, dtype=np.int16).astype(np.float32) / 32768.0
    
    results = MODEL.transcribe(
        audio=arr,
        language=language,  # None 为自动检测
        sample_rate=16000,
    )
    
    return results[0].text if results else ""


async def handle(websocket):
    log.info("Client connected: %s", websocket.remote_address)
    buf = bytearray()
    language = None
    loop = asyncio.get_event_loop()

    try:
        async for message in websocket:
            if isinstance(message, str):
                data = json.loads(message)
                if "mode" in data:
                    # 配置模式
                    language = data.get("language")  # 如 "zh", "en", None
                    log.info("Config: language=%s", language)
                elif data.get("is_speaking") is False:
                    # 结束识别
                    if buf:
                        text = await loop.run_in_executor(None, run_asr, bytes(buf), language)
                        await websocket.send(json.dumps({
                            "text": text,
                            "is_final": True,
                            "mode": "offline",
                        }))
                        log.info("Final: %r", text)
                    buf.clear()
                    
            elif isinstance(message, bytes):
                # 音频数据
                buf.extend(message)
                if len(buf) >= PARTIAL_THRESHOLD_BYTES:
                    # 流式识别（可选）
                    text = await loop.run_in_executor(None, run_asr, bytes(buf), language)
                    await websocket.send(json.dumps({
                        "text": text,
                        "is_final": False,
                        "mode": "streaming",
                    }))
    except websockets.exceptions.ConnectionClosed:
        log.info("Client disconnected")
    except Exception:
        log.exception("Handler error")


async def main(host: str, port: int):
    async with websockets.serve(handle, host, port, max_size=16 * 1024 * 1024):
        log.info("Qwen3-ASR WebSocket listening on ws://%s:%d", host, port)
        await asyncio.Future()


if __name__ == "__main__":
    p = argparse.ArgumentParser(description="Qwen3-ASR vLLM WebSocket Server")
    p.add_argument("--model", required=True, help="模型路径或HuggingFace ID")
    p.add_argument("--host", default="0.0.0.0")
    p.add_argument("--port", type=int, default=10096)
    p.add_argument("--gpu-util", type=float, default=0.75, help="GPU显存占用比例（推荐 0.75-0.9）")
    p.add_argument(
        "--max-model-len",
        type=int,
        default=1024,
        help="限制 KV cache 长度，避免显存不足（推荐 512/1024）",
    )
    p.add_argument(
        "--backend",
        choices=["auto", "vllm", "transformers"],
        default="auto",
        help="推理后端：auto(优先vllm失败回退) / vllm / transformers",
    )
    args = p.parse_args()

    try:
        MODEL = load_model(args.model, args.gpu_util, args.max_model_len, args.backend)
    except Exception:
        log.exception("Failed to initialize ASR model")
        sys.exit(1)
    asyncio.run(main(args.host, args.port))
