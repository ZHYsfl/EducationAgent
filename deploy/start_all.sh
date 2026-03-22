#!/usr/bin/env bash
# Start all Voice Agent model services in the background.
# Logs go to ~/voice-logs/. PIDs saved to ~/voice-logs/pids.
# Usage: bash start_all.sh

set -e

REPO="/mnt/d/创业/myResearch/EducationAgent"
VENV="$HOME/.venv/bin/activate"
COSYVOICE_REPO="$HOME/voice-services/CosyVoice"
REF_WAV="$COSYVOICE_REPO/asset/zero_shot_prompt.wav"
LOG_DIR="$HOME/voice-logs"

mkdir -p "$LOG_DIR"
source "$VENV"

echo "[1/3] Starting vLLM (port 8000)..."
nohup vllm serve "$REPO/models/Qwen3___5-0___8B" \
  --port 8000 \
  --host 0.0.0.0 \
  --gpu-memory-utilization 0.35 \
  --max-model-len 8192 \
  --dtype auto \
  > "$LOG_DIR/vllm.log" 2>&1 &
echo $! > "$LOG_DIR/vllm.pid"
echo "    PID=$(cat $LOG_DIR/vllm.pid)  log=$LOG_DIR/vllm.log"

echo "[2/3] Starting Qwen3-ASR WebSocket (port 10096)..."
nohup python3 "$REPO/deploy/start_asr_vllm.py" \
  --model "$REPO/models/asr" \
  --port 10096 \
  --gpu-util 0.25 \
  > "$LOG_DIR/asr.log" 2>&1 &
echo $! > "$LOG_DIR/asr.pid"
echo "    PID=$(cat $LOG_DIR/asr.pid)  log=$LOG_DIR/asr.log"

echo "[3/3] Starting CosyVoice3 TTS FastAPI (port 50000)..."
COSYVOICE_REPO="$COSYVOICE_REPO" REF_WAV="$REF_WAV" \
nohup python3 "$REPO/deploy/start_tts.py" \
  --model-dir "$REPO/models/tts" \
  --port 50000 \
  > "$LOG_DIR/tts.log" 2>&1 &
echo $! > "$LOG_DIR/tts.pid"
echo "    PID=$(cat $LOG_DIR/tts.pid)  log=$LOG_DIR/tts.log"

echo ""
echo "All services started in background. Terminal is free."
echo "Check logs:  tail -f $LOG_DIR/vllm.log"
echo "Stop all:    bash $REPO/deploy/stop_all.sh"
