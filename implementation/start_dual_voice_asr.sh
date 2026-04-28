#!/bin/bash
# =============================================================================
# 单卡同时启动三路 vLLM（默认端口与 service/.env 一致）：
#   8001 — 语音 Qwen 4B + LoRA（voice-agent）
#   8002 — Qwen3-ASR
#   8000 — 打断检测：基座 + LoRA（默认 model id: interrupt-detection）
#
# - 语音 / 打断检测 用 /root/.venv；ASR 用 /root/autodl-tmp/.venv（勿混用）
# - 启动顺序：语音 → 等 /health → 打断检测 → 等 /health → ASR → 等 /health
#
# 端口（可用环境变量覆盖）:
#   VOICE_PORT / INTERRUPT_PORT / ASR_PORT
#
# 用法:
#   bash start_dual_voice_asr.sh              # 前台等待三路子进程；Ctrl+C 会结束子进程
#   bash start_dual_voice_asr.sh detach       # 拉起后立刻退出 shell，服务仍在后台
#   bash start_dual_voice_asr.sh screen       # screen 后台会话（默认名 vllm_dual）
#   bash start_dual_voice_asr.sh stop         # 按 logs/*.pid 结束三路 vLLM
#
# 显存（同卡三路默认之和约 0.85，请按 nvidia-smi 微调）:
#   VOICE_GPU_MEMORY_UTILIZATION     默认 0.38
#   INTERRUPT_GPU_MEMORY_UTILIZATION 默认 0.17
#   ASR_GPU_MEMORY_UTILIZATION       默认 0.30
#
# 其他默认：VOICE_MAX_NUM_SEQS=64；INTERRUPT_MAX_NUM_SEQS=32；ASR_MAX_MODEL_LEN=2048。
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_DIR="${DUAL_VLLM_LOG_DIR:-$SCRIPT_DIR/logs}"
mkdir -p "$LOG_DIR"
VOICE_PID_FILE="$LOG_DIR/voice_vllm.pid"
INTERRUPT_PID_FILE="$LOG_DIR/interrupt_vllm.pid"
ASR_PID_FILE="$LOG_DIR/asr_vllm.pid"
VOICE_LOG="$LOG_DIR/voice_vllm.log"
INTERRUPT_LOG="$LOG_DIR/interrupt_vllm.log"
ASR_LOG="$LOG_DIR/asr_vllm.log"

VOICE_PY="${VOICE_VENV_PYTHON:-/root/.venv/bin/python}"
ASR_PY="${ASR_VENV_PYTHON:-/root/autodl-tmp/.venv/bin/python}"

BASE_MODEL_PATH="${VOICE_BASE_MODEL_PATH:-/root/autodl-tmp/train}"
LORA_ADAPTER_PATH="${VOICE_LORA_ADAPTER_PATH:-/root/autodl-tmp/output/final}"

INTERRUPT_BASE="${INTERRUPT_BASE_MODEL_PATH:-/root/autodl-tmp/interrupt-check}"
INTERRUPT_LORA="${INTERRUPT_LORA_PATH:-/root/autodl-tmp/interrupt-check-lora/interrupt_detection_cot_lora}"
INTERRUPT_LORA_NAME="${INTERRUPT_LORA_NAME:-interrupt-detection}"

ASR_MODEL="${ASR_MODEL_PATH:-/root/autodl-tmp/asr}"

VOICE_PORT="${VOICE_PORT:-8001}"
INTERRUPT_PORT="${INTERRUPT_PORT:-8000}"
ASR_PORT="${ASR_PORT:-8002}"

# 同卡三路：显存占比之和约 0.85
GPU_MEM_VOICE="${VOICE_GPU_MEMORY_UTILIZATION:-0.38}"
GPU_MEM_INTERRUPT="${INTERRUPT_GPU_MEMORY_UTILIZATION:-0.17}"
GPU_MEM_ASR="${ASR_GPU_MEMORY_UTILIZATION:-0.30}"
VOICE_SEQS="${VOICE_MAX_NUM_SEQS:-64}"
INTERRUPT_SEQS="${INTERRUPT_MAX_NUM_SEQS:-32}"
ASR_MAX_LEN="${ASR_MAX_MODEL_LEN:-2048}"
INTERRUPT_MAX_LEN="${INTERRUPT_MAX_MODEL_LEN:-1536}"

WAIT_VOICE_READY_SEC="${WAIT_VOICE_READY_SEC:-420}"
WAIT_INTERRUPT_READY_SEC="${WAIT_INTERRUPT_READY_SEC:-420}"

if [ "${1:-}" = "stop" ]; then
    for f in "$VOICE_PID_FILE" "$INTERRUPT_PID_FILE" "$ASR_PID_FILE"; do
        if [ -f "$f" ]; then
            pid="$(cat "$f")"
            if kill -0 "$pid" 2>/dev/null; then
                echo "[INFO] kill pid=$pid ($f)"
                kill "$pid" 2>/dev/null || true
            fi
            rm -f "$f"
        fi
    done
    echo "[OK] stop 已发送（若仍有 vllm 进程请 nvidia-smi 查 PID 手动 kill）"
    exit 0
fi

# screen 后台会话：SSH 断开后服务继续跑；重连后 screen -r <会话名>
if [ "${1:-}" = "screen" ]; then
    shift
    SESSION="${SCREEN_SESSION_NAME:-vllm_dual}"
    SELF="$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")"
    echo "[INFO] 启动 screen 会话: $SESSION  →  $SELF $*"
    exec screen -dmS "$SESSION" bash "$SELF" "$@"
fi

DETACH=0
if [ "${1:-}" = "detach" ]; then
    DETACH=1
    shift
fi

if [ ! -x "$VOICE_PY" ]; then
    echo "[ERROR] 语音/打断检测环境不存在: $VOICE_PY"
    exit 1
fi
if [ ! -x "$ASR_PY" ]; then
    echo "[ERROR] ASR 环境不存在: $ASR_PY"
    exit 1
fi
if [ ! -d "$BASE_MODEL_PATH" ] || [ ! -d "$LORA_ADAPTER_PATH" ]; then
    echo "[ERROR] 基座或 LoRA 不存在: $BASE_MODEL_PATH / $LORA_ADAPTER_PATH"
    exit 1
fi
if [ ! -d "$INTERRUPT_BASE" ] || [ ! -d "$INTERRUPT_LORA" ]; then
    echo "[ERROR] 打断检测基座或 LoRA 不存在: $INTERRUPT_BASE / $INTERRUPT_LORA"
    exit 1
fi
if [ ! -d "$ASR_MODEL" ]; then
    echo "[ERROR] ASR 模型目录不存在: $ASR_MODEL"
    exit 1
fi

echo "=========================================="
echo "[INFO] 同卡三服务  语音   gpu-mem=$GPU_MEM_VOICE max-seqs=$VOICE_SEQS :$VOICE_PORT"
echo "[INFO]             打断检测 gpu-mem=$GPU_MEM_INTERRUPT max-seqs=$INTERRUPT_SEQS :$INTERRUPT_PORT"
echo "[INFO]             ASR    gpu-mem=$GPU_MEM_ASR max-len=$ASR_MAX_LEN :$ASR_PORT"
echo "[INFO] 日志: $VOICE_LOG"
echo "[INFO]       $INTERRUPT_LOG"
echo "[INFO]       $ASR_LOG"
echo "=========================================="

wait_http_200() {
    local url=$1
    local deadline=$2
    local t=0
    while [ "$t" -lt "$deadline" ]; do
        if command -v curl >/dev/null 2>&1; then
            if curl -sf "$url" >/dev/null 2>&1; then
                return 0
            fi
        else
            if command -v wget >/dev/null 2>&1; then
                if wget -q -O /dev/null "$url" 2>/dev/null; then
                    return 0
                fi
            fi
        fi
        sleep 3
        t=$((t + 3))
        echo "  ... 等待就绪 ${t}s / ${deadline}s  $url"
    done
    return 1
}

cleanup() {
    echo ""
    echo "[INFO] 收到退出信号，结束子进程..."
    if [ -n "${VOICE_PID:-}" ] && kill -0 "$VOICE_PID" 2>/dev/null; then
        kill "$VOICE_PID" 2>/dev/null || true
    fi
    if [ -n "${INTERRUPT_PID:-}" ] && kill -0 "$INTERRUPT_PID" 2>/dev/null; then
        kill "$INTERRUPT_PID" 2>/dev/null || true
    fi
    if [ -n "${ASR_PID:-}" ] && kill -0 "$ASR_PID" 2>/dev/null; then
        kill "$ASR_PID" 2>/dev/null || true
    fi
}
trap cleanup INT TERM

# 1) 语音 Qwen 4B
nohup "$VOICE_PY" -m vllm.entrypoints.openai.api_server \
    --model "$BASE_MODEL_PATH" \
    --enable-lora \
    --lora-modules voice-agent="$LORA_ADAPTER_PATH" \
    --max-model-len 1536 \
    --dtype bfloat16 \
    --gpu-memory-utilization "$GPU_MEM_VOICE" \
    --max-num-seqs "$VOICE_SEQS" \
    --max-loras 1 \
    --max-cpu-loras 2 \
    --enforce-eager \
    --host 0.0.0.0 \
    --port "$VOICE_PORT" \
    >>"$VOICE_LOG" 2>&1 &
VOICE_PID=$!
echo "$VOICE_PID" >"$VOICE_PID_FILE"
echo "[INFO] Voice (Qwen 4B)  pid=$VOICE_PID  日志 $VOICE_LOG"

if ! wait_http_200 "http://127.0.0.1:${VOICE_PORT}/health" "$WAIT_VOICE_READY_SEC"; then
    echo "[ERROR] 语音服务在 ${WAIT_VOICE_READY_SEC}s 内未就绪，请查 $VOICE_LOG"
    kill "$VOICE_PID" 2>/dev/null || true
    rm -f "$VOICE_PID_FILE"
    exit 1
fi
echo "[OK] 语音服务已就绪 http://127.0.0.1:${VOICE_PORT}/health"

# 2) 打断检测（同 venv 与语音）
nohup "$VOICE_PY" -m vllm.entrypoints.openai.api_server \
    --model "$INTERRUPT_BASE" \
    --enable-lora \
    --lora-modules "${INTERRUPT_LORA_NAME}=$INTERRUPT_LORA" \
    --max-model-len "$INTERRUPT_MAX_LEN" \
    --dtype bfloat16 \
    --gpu-memory-utilization "$GPU_MEM_INTERRUPT" \
    --max-num-seqs "$INTERRUPT_SEQS" \
    --max-loras 1 \
    --max-cpu-loras 2 \
    --enforce-eager \
    --host 0.0.0.0 \
    --port "$INTERRUPT_PORT" \
    >>"$INTERRUPT_LOG" 2>&1 &
INTERRUPT_PID=$!
echo "$INTERRUPT_PID" >"$INTERRUPT_PID_FILE"
echo "[INFO] Interrupt detection  pid=$INTERRUPT_PID  日志 $INTERRUPT_LOG"

if ! wait_http_200 "http://127.0.0.1:${INTERRUPT_PORT}/health" "$WAIT_INTERRUPT_READY_SEC"; then
    echo "[ERROR] 打断检测在 ${WAIT_INTERRUPT_READY_SEC}s 内未就绪，请查 $INTERRUPT_LOG"
    kill "$VOICE_PID" "$INTERRUPT_PID" 2>/dev/null || true
    rm -f "$VOICE_PID_FILE" "$INTERRUPT_PID_FILE"
    exit 1
fi
echo "[OK] 打断检测已就绪 http://127.0.0.1:${INTERRUPT_PORT}/health"

# 3) ASR Qwen3
nohup "$ASR_PY" -m vllm.entrypoints.openai.api_server \
    --model "$ASR_MODEL" \
    --dtype bfloat16 \
    --max-model-len "$ASR_MAX_LEN" \
    --gpu-memory-utilization "$GPU_MEM_ASR" \
    --enforce-eager \
    --host 0.0.0.0 \
    --port "$ASR_PORT" \
    >>"$ASR_LOG" 2>&1 &
ASR_PID=$!
echo "$ASR_PID" >"$ASR_PID_FILE"
echo "[INFO] ASR (Qwen3-ASR) pid=$ASR_PID  日志 $ASR_LOG"

if ! wait_http_200 "http://127.0.0.1:${ASR_PORT}/health" 600; then
    echo "[WARN] ASR 在 600s 内未返回 /health，请查 $ASR_LOG（可能仍在编译/加载）"
fi

echo ""
echo "[OK] 三路已启动。"
echo "    语音 API: http://127.0.0.1:${VOICE_PORT}/v1   model: voice-agent"
echo "    打断检测: http://127.0.0.1:${INTERRUPT_PORT}/v1   model: ${INTERRUPT_LORA_NAME}"
echo "    ASR  API: http://127.0.0.1:${ASR_PORT}/v1   model: $ASR_MODEL"
echo "    停止: bash $SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}") stop"
echo ""

if [ "$DETACH" = "1" ]; then
    echo "[OK] detach 模式：本 shell 已退出，vLLM 仍在后台。日志见上述三份。"
    exit 0
fi

echo "[INFO] 前台等待子进程（Ctrl+C 结束三路）。若在 screen 里可先 Ctrl+A D detach。"
wait "$VOICE_PID" "$INTERRUPT_PID" "$ASR_PID" || true
