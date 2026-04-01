FROM python:3.11-slim

WORKDIR /app

# 预装系统依赖（EasyOCR / Whisper 需要）
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1-mesa-glx \
    libglib2.0-0 \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8888

# 生产用 gunicorn，2 个 worker 进程
CMD ["gunicorn", "-w", "2", "-b", "0.0.0.0:8888", "api.server:app"]
