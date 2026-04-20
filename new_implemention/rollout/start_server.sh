#!/bin/bash
# =============================================================================
# Voice Agent vLLM 推理服务启动脚本 (OpenAI-compatible API + LoRA)
# =============================================================================
# 用法:
#   1. 填写下方 BASE_MODEL_PATH 和 LORA_ADAPTER_PATH
#   2. bash start_server.sh
#   3. 服务默认监听 0.0.0.0:8000
#
# 客户端用 OpenAI SDK 访问，model 名填 "voice-agent"
# =============================================================================

# TODO: 填写本地绝对路径
BASE_MODEL_PATH=""        # 例: /root/autodl-tmp/Qwen3.5-4B-Instruct
LORA_ADAPTER_PATH=""      # 例: /root/autodl-tmp/output/final

if [ -z "$BASE_MODEL_PATH" ] || [ -z "$LORA_ADAPTER_PATH" ]; then
    echo "[ERROR] 请先编辑本脚本，填写 BASE_MODEL_PATH 和 LORA_ADAPTER_PATH"
    exit 1
fi

python -m vllm.entrypoints.openai.api_server \
    --model "$BASE_MODEL_PATH" \
    --enable-lora \
    --lora-modules voice-agent="$LORA_ADAPTER_PATH" \
    --max-model-len 1536 \
    --dtype bfloat16 \
    --gpu-memory-utilization 0.85 \
    --max-num-seqs 64 \
    --max-loras 1 \
    --max-cpu-loras 2 \
    --enforce-eager \
    --host 0.0.0.0 \
    --port 8000

# 4090 32GB 单卡保守说明:
#   --gpu-memory-utilization 0.85 : 给 KV cache 留足余量，避免长序列/高并发 OOM
#   --max-num-seqs 64             : 限制并发序列数，控制 KV cache 峰值
#   --enforce-eager               : 关闭 CUDA graph，省显存、启动更快（略降吞吐）
#   --max-loras 1                 : 只同时加载 1 个 LoRA（当前场景足够）
#
# 如需 API Key 鉴证，加上: --api-key your-secret-key
