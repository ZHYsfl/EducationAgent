#!/bin/bash
# =============================================================================
# 单卡同时启动：Qwen 4B 语音 vLLM+LoRA（6008） + Qwen3-ASR（6006）
# 本仓库仅此脚本负责拉起上述两路 vLLM。
#
# - 语音用 /root/.venv，ASR 用 /root/autodl-tmp/.venv（勿混用）
# - 先起语音，等 /health 就绪再起 ASR，避免双进程同时抢显存加载导致失败
#
# 端口（可用 VOICE_PORT / ASR_PORT 覆盖）:
#   6008 — 语音 vLLM（OpenAI API /v1，model: voice-agent）
#   6006 — Qwen3-ASR vLLM（/v1）
#
# 用法:
#   bash start_dual_voice_asr.sh              # 当前终端前台等待；Ctrl+C 会结束两路子进程
#   bash start_dual_voice_asr.sh detach       # 拉起后立刻退出 shell，两路仍在后台（日志见 logs/）
#   bash start_dual_voice_asr.sh screen       # 用 screen 后台会话跑「前台等待」模式，SSH 断线不关服
#   SCREEN_SESSION_NAME=myvllm bash ... screen   # 自定义 screen 会话名（默认 vllm_dual）
#   bash start_dual_voice_asr.sh stop         # 按 logs/*.pid 结束两路 vLLM
#
# screen 持久化示例（断线后可 screen -r vllm_dual 重新接入）:
#   cd /root/autodl-tmp && bash start_dual_voice_asr.sh screen
#   # 或交互: screen -S vllm_dual  →  cd ... && bash start_dual_voice_asr.sh  →  Ctrl+A D detach
#
# 显存比例（同卡双开默认之和 0.85）:
#   VOICE_GPU_MEMORY_UTILIZATION  默认 0.47
#   ASR_GPU_MEMORY_UTILIZATION    默认 0.38
#   改一侧时请自行控制二者之和（建议 ≤0.88，留余量）。
#
# 其他默认：VOICE_MAX_NUM_SEQS=64；ASR_MAX_MODEL_LEN=2048。
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_DIR="${DUAL_VLLM_LOG_DIR:-$SCRIPT_DIR/logs}"
mkdir -p "$LOG_DIR"
VOICE_PID_FILE="$LOG_DIR/voice_vllm.pid"
ASR_PID_FILE="$LOG_DIR/asr_vllm.pid"
VOICE_LOG="$LOG_DIR/voice_vllm.log"
ASR_LOG="$LOG_DIR/asr_vllm.log"

VOICE_PY="${VOICE_VENV_PYTHON:-/root/.venv/bin/python}"
ASR_PY="${ASR_VENV_PYTHON:-/root/autodl-tmp/.venv/bin/python}"

BASE_MODEL_PATH="${VOICE_BASE_MODEL_PATH:-/root/autodl-tmp/train}"
LORA_ADAPTER_PATH="${VOICE_LORA_ADAPTER_PATH:-/root/autodl-tmp/output/final}"
ASR_MODEL="${ASR_MODEL_PATH:-/root/autodl-tmp/asr}"

VOICE_PORT="${VOICE_PORT:-6008}"
ASR_PORT="${ASR_PORT:-6006}"

# 同卡默认：与单独脚本参数对齐，利用率之和 0.85（0.47+0.38）
GPU_MEM_VOICE="${VOICE_GPU_MEMORY_UTILIZATION:-0.47}"
GPU_MEM_ASR="${ASR_GPU_MEMORY_UTILIZATION:-0.38}"
VOICE_SEQS="${VOICE_MAX_NUM_SEQS:-64}"
ASR_MAX_LEN="${ASR_MAX_MODEL_LEN:-2048}"

WAIT_VOICE_READY_SEC="${WAIT_VOICE_READY_SEC:-420}"

if [ "${1:-}" = "stop" ]; then
    for f in "$VOICE_PID_FILE" "$ASR_PID_FILE"; do
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
    echo "[ERROR] 语音环境不存在: $VOICE_PY"
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
if [ ! -d "$ASR_MODEL" ]; then
    echo "[ERROR] ASR 模型目录不存在: $ASR_MODEL"
    exit 1
fi

echo "=========================================="
echo "[INFO] 同卡双服务  语音 gpu-mem=$GPU_MEM_VOICE max-seqs=$VOICE_SEQS :$VOICE_PORT"
echo "[INFO]               ASR  gpu-mem=$GPU_MEM_ASR max-len=$ASR_MAX_LEN :$ASR_PORT"
echo "[INFO] 日志: $VOICE_LOG"
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

# 2) ASR Qwen3
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
echo "[OK] 两路已启动。"
echo "    语音 API: http://127.0.0.1:${VOICE_PORT}/v1   model: voice-agent"
echo "    ASR  API: http://127.0.0.1:${ASR_PORT}/v1   model: $ASR_MODEL"
echo "    停止: bash $SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}") stop"
echo ""

if [ "$DETACH" = "1" ]; then
    echo "[OK] detach 模式：本 shell 已退出，vLLM 仍在后台。日志: $VOICE_LOG 与 $ASR_LOG"
    exit 0
fi

echo "[INFO] 前台等待子进程（Ctrl+C 结束两路）。若在 screen 里可先 Ctrl+A D detach。"
wait "$VOICE_PID" "$ASR_PID" || true
