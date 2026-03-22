#!/usr/bin/env bash
# Stop all Voice Agent model services.
# Usage: bash stop_all.sh

LOG_DIR="$HOME/voice-logs"

stop_service() {
    local name=$1
    local pid_file="$LOG_DIR/$name.pid"
    if [ -f "$pid_file" ]; then
        PID=$(cat "$pid_file")
        if kill -0 "$PID" 2>/dev/null; then
            kill "$PID"
            echo "Stopped $name (PID $PID)"
        else
            echo "$name (PID $PID) was not running"
        fi
        rm -f "$pid_file"
    else
        echo "No PID file for $name"
    fi
}

stop_service vllm
stop_service asr
stop_service tts

echo "Done."
