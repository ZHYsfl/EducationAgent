# EducationAgent Linux 部署指南

本文档描述如何将 EducationAgent 项目完整部署到 Linux 服务器（含前端静态文件服务）。

---

## 一、服务架构与端口

| 服务 | 目录 | 端口 | 说明 |
|------|------|------|------|
| Voice Agent v2 | `voice_agent_v2/` | 9000 | 主入口，WebSocket + HTTP REST，托管 `static/` 前端 |
| PPT Agent | `zcxppt/` | 9400 | PPT 生成、修改、导出服务 |
| KB Service | `kb-service/` | 9200 | 知识库管理、向量检索服务 |
| Memory Service | `memory_service/` | 9300 | 记忆召回、工作记忆、用户画像 |
| Interface | `interface/` | 9500 | 搜索代理、多模态接口、OSS 代理 |
| Auth Service | `auth_service/` | 9300 | 认证服务（可与 Memory 共用端口或独立部署） |

**基础设施依赖：**

| 组件 | 默认端口 | 说明 |
|------|----------|------|
| PostgreSQL | 5432 | auth、memory、kb、interface 共用 |
| Redis | 6379 | zcxppt 任务队列、缓存 |
| Qdrant | 6333 | kb-service 向量数据库 |

**AI 模型服务（可选本地或远程）：**

| 服务 | 端口 | 说明 |
|------|------|------|
| vLLM | 8000 | Small + Large LLM 共用实例 |
| ASR | 10096 | Qwen3-ASR WebSocket 服务 |
| TTS | 50000 | CosyVoice FastAPI 服务 |

> 若不想本地部署 GPU 服务，可将 `.env` 中 `ASR_MODE` / `TTS_MODE` 设为 `remote`，走豆包云端 API。

---

## 二、环境准备

### 2.1 系统要求

- Ubuntu 22.04 LTS 或兼容发行版
- Go 1.23+
- PostgreSQL 15+
- Redis 7+
- （可选）NVIDIA GPU + CUDA 12.8+（本地运行 vLLM/ASR/TTS）

### 2.2 安装基础依赖

```bash
sudo apt update && sudo apt install -y \
  git curl wget build-essential ca-certificates \
  postgresql postgresql-contrib redis-server \
  nginx systemd

# Go（如未安装，示例安装 1.23）
wget https://go.dev/dl/go1.23.4.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.23.4.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc
source ~/.bashrc
go version
```

### 2.3 启动基础服务

```bash
# PostgreSQL
sudo systemctl enable postgresql
sudo systemctl start postgresql

# Redis
sudo systemctl enable redis-server
sudo systemctl start redis-server
```

### 2.4 创建数据库与用户

```bash
sudo -u postgres psql <<'EOF'
CREATE USER eduagent WITH PASSWORD 'eduagent_password';
CREATE DATABASE eduagent OWNER eduagent;
CREATE DATABASE kbdb OWNER eduagent;
GRANT ALL PRIVILEGES ON DATABASE eduagent TO eduagent;
GRANT ALL PRIVILEGES ON DATABASE kbdb TO eduagent;
EOF
```

### 2.5 部署 Qdrant（Docker 方式，推荐）

```bash
# 安装 Docker（如未安装）
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker $USER
newgrp docker

# 运行 Qdrant
docker run -d --name qdrant \
  -p 6333:6333 -p 6334:6334 \
  -v $(pwd)/qdrant_storage:/qdrant/storage \
  qdrant/qdrant:latest
```

---

## 三、项目编译

假设项目代码已克隆到 `/opt/education-agent`：

```bash
cd /opt/education-agent

# 编译各服务
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/voice_agent_v2 ./voice_agent_v2/main.go
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/zcxppt ./zcxppt/cmd/server/main.go
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/kb-service ./kb-service/main.go
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/interface ./interface/cmd/server/main.go

# auth_service / memory_service 如已有 cmd/server/main.go 则同样编译：
# GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/auth_service ./auth_service/cmd/server/main.go
# GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/memory_service ./memory_service/cmd/server/main.go

mkdir -p bin
cp -r voice_agent_v2/static bin/static
```

---

## 四、环境变量配置

在项目根目录创建 `.env`，各服务会按优先级读取（部分服务支持 `godotenv.Load()`）。

### 4.1 根目录 `.env` 示例

```env
# ==========================================
# 通用基础设施
# ==========================================
POSTGRES_DSN=postgres://eduagent:eduagent_password@localhost:5432/eduagent?sslmode=disable
PG_DSN=postgres://eduagent:eduagent_password@localhost:5432/kbdb?sslmode=disable
DATABASE_URL=postgres://eduagent:eduagent_password@localhost:5432/eduagent?sslmode=disable

REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
REDIS_DB=0

QDRANT_URL=http://localhost:6333

# ==========================================
# 安全密钥（生产环境务必修改）
# ==========================================
JWT_SECRET=your-very-strong-jwt-secret-change-me
INTERNAL_KEY=your-internal-service-key

# ==========================================
# LLM 配置（本地 vLLM 示例）
# ==========================================
SMALL_LLM_BASE_URL=http://localhost:8000/v1
SMALL_LLM_MODEL=/models/Qwen3___5-0___8B
SMALL_LLM_API_KEY=EMPTY

LARGE_LLM_BASE_URL=http://localhost:8000/v1
LARGE_LLM_MODEL=/models/Qwen3___5-0___8B
LARGE_LLM_API_KEY=EMPTY

LLM_BASE_URL=https://api.moonshot.cn/v1
LLM_MODEL=kimi-k2.5
LLM_API_KEY=sk-your-moonshot-key

# ==========================================
# ASR / TTS
# ==========================================
ASR_MODE=local
ASR_WS_URL=ws://localhost:10096

TTS_MODE=local
TTS_URL=http://localhost:50000

# 远程豆包配置（当 ASR_MODE/TTS_MODE=remote 时生效）
DOUBAO_ASR_APP_KEY=
DOUBAO_ASR_ACCESS_KEY=
DOUBAO_ASR_RESOURCE_ID=volc.seedasr.sauc.duration

DOUBAO_TTS_APPID=
DOUBAO_TTS_TOKEN=
DOUBAO_TTS_CLUSTER=volcano_tts
DOUBAO_TTS_VOICE_TYPE=zh_female_yv_uranus_bigtts

# ==========================================
# 服务间调用地址
# ==========================================
SERVER_PORT=9000
ZCXPPT_PORT=9400
PORT=9200
MEMORY_PORT=9300
AUTH_PORT=9300

VOICE_AGENT_URL=http://localhost:9000
PPT_AGENT_URL=http://localhost:9400
KB_SERVICE_URL=http://localhost:9200
MEMORY_SERVICE_URL=http://localhost:9300
SEARCH_SERVICE_URL=http://localhost:9500
DB_SERVICE_URL=http://localhost:9500
VOICE_AGENT_BASE_URL=http://localhost:9000

# ==========================================
# OSS / 文件存储
# ==========================================
OSS_PROVIDER=local
OSS_BASE_URL=http://localhost:9500
OSS_LOCAL_PATH=/opt/education-agent/storage
OSS_ALLOW_UNSIGNED=false

# ==========================================
# Voice Agent 交互参数
# ==========================================
TOKEN_BUDGET=50
FILLER_INTERVAL=100
FILLER_PHRASE_1=好的，让我想一下
FILLER_PHRASE_2=还在想，稍等一下
FILLER_PHRASE_3=马上就好
SYSTEM_PROMPT=你是一个有帮助的AI教育助手，请用中文回答问题。回答时请先给一句简短回应，再展开详细解释。第一句避免长句和复杂从句。
```

---

## 五、Systemd 服务配置

将以下服务文件放置到 `/etc/systemd/system/`，并调整 `WorkingDirectory` 和 `EnvironmentFile` 路径。

### 5.1 voice-agent-v2.service

```ini
[Unit]
Description=Voice Agent v2
After=network.target postgresql.service redis-server.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/education-agent
EnvironmentFile=/opt/education-agent/.env
ExecStart=/opt/education-agent/bin/voice_agent_v2
Restart=always
RestartSec=5
StandardOutput=append:/var/log/education-agent/voice_agent_v2.log
StandardError=append:/var/log/education-agent/voice_agent_v2.log

[Install]
WantedBy=multi-user.target
```

### 5.2 zcxppt.service

```ini
[Unit]
Description=PPT Agent Service
After=network.target redis-server.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/education-agent/zcxppt
EnvironmentFile=/opt/education-agent/.env
ExecStart=/opt/education-agent/bin/zcxppt
Restart=always
RestartSec=5
StandardOutput=append:/var/log/education-agent/zcxppt.log
StandardError=append:/var/log/education-agent/zcxppt.log

[Install]
WantedBy=multi-user.target
```

### 5.3 kb-service.service

```ini
[Unit]
Description=KB Service
After=network.target postgresql.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/education-agent/kb-service
EnvironmentFile=/opt/education-agent/.env
ExecStart=/opt/education-agent/bin/kb-service
Restart=always
RestartSec=5
StandardOutput=append:/var/log/education-agent/kb-service.log
StandardError=append:/var/log/education-agent/kb-service.log

[Install]
WantedBy=multi-user.target
```

### 5.4 interface.service

```ini
[Unit]
Description=Interface Service
After=network.target postgresql.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/education-agent/interface
EnvironmentFile=/opt/education-agent/.env
ExecStart=/opt/education-agent/bin/interface
Restart=always
RestartSec=5
StandardOutput=append:/var/log/education-agent/interface.log
StandardError=append:/var/log/education-agent/interface.log

[Install]
WantedBy=multi-user.target
```

### 5.5 memory-service.service（如已编译）

```ini
[Unit]
Description=Memory Service
After=network.target postgresql.service redis-server.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/education-agent/memory_service
EnvironmentFile=/opt/education-agent/.env
ExecStart=/opt/education-agent/bin/memory_service
Restart=always
RestartSec=5
StandardOutput=append:/var/log/education-agent/memory_service.log
StandardError=append:/var/log/education-agent/memory_service.log

[Install]
WantedBy=multi-user.target
```

### 5.6 auth-service.service（如已编译）

```ini
[Unit]
Description=Auth Service
After=network.target postgresql.service

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/education-agent/auth_service
EnvironmentFile=/opt/education-agent/.env
ExecStart=/opt/education-agent/bin/auth_service
Restart=always
RestartSec=5
StandardOutput=append:/var/log/education-agent/auth_service.log
StandardError=append:/var/log/education-agent/auth_service.log

[Install]
WantedBy=multi-user.target
```

### 5.7 应用 systemd 配置

```bash
sudo mkdir -p /var/log/education-agent
sudo chown -R www-data:www-data /var/log/education-agent
sudo chown -R www-data:www-data /opt/education-agent

sudo systemctl daemon-reload

# 启动服务
sudo systemctl enable voice-agent-v2 zcxppt kb-service interface
sudo systemctl start voice-agent-v2 zczcxppt kb-service interface

# 查看状态
sudo systemctl status voice-agent-v2
```

---

## 六、Nginx 反向代理配置

以下配置将 80/443 端口统一代理到 Voice Agent v2（9000），同时暴露各服务的 API。

`/etc/nginx/sites-available/education-agent`：

```nginx
upstream voice_agent {
    server 127.0.0.1:9000;
}

upstream zcxppt {
    server 127.0.0.1:9400;
}

upstream kb_service {
    server 127.0.0.1:9200;
}

upstream interface_service {
    server 127.0.0.1:9500;
}

upstream memory_service {
    server 127.0.0.1:9300;
}

server {
    listen 80;
    server_name your-domain.com;

    client_max_body_size 100M;

    # Voice Agent v2 主站（含 static 前端）
    location / {
        proxy_pass http://voice_agent;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 86400;
    }

    # WebSocket 专用（Voice Agent）
    location /ws {
        proxy_pass http://voice_agent;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400;
    }

    # 各服务 API 路由
    location /api/v1/ppt/ {
        proxy_pass http://zcxppt;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/v1/teaching-plan/ {
        proxy_pass http://zcxppt;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/v1/content-diversity/ {
        proxy_pass http://zcxppt;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/v1/export/ {
        proxy_pass http://zcxppt;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/v1/kb/ {
        proxy_pass http://kb_service;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/v1/memory/ {
        proxy_pass http://memory_service;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/v1/auth/ {
        proxy_pass http://memory_service;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # interface 的 OSS / search 等接口
    location /api/v1/search {
        proxy_pass http://interface_service;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /oss/ {
        proxy_pass http://interface_service;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    # 静态文件缓存优化
    location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2)$ {
        proxy_pass http://voice_agent;
        expires 1d;
        add_header Cache-Control "public, immutable";
    }
}
```

启用站点：

```bash
sudo ln -sf /etc/nginx/sites-available/education-agent /etc/nginx/sites-enabled/
sudo nginx -t
sudo systemctl reload nginx
```

---

## 七、前端静态文件说明

Voice Agent v2 的前端文件位于 `voice_agent_v2/static/`，包含：

- `index.html` - 主页面
- `app.js` - 前端逻辑
- `styles.css` - 样式文件

服务启动后，`/` 路由自动返回 `static/index.html`，`/static/*` 自动提供静态资源。**无需额外 Nginx 静态目录配置**，除非你想让 Nginx 直接 serving 以减轻 Go 服务压力。

如需 Nginx 直接托管静态文件，在 `location /` 前插入：

```nginx
location /static/ {
    alias /opt/education-agent/voice_agent_v2/static/;
    expires 1d;
}

location = / {
    root /opt/education-agent/voice_agent_v2/static;
    try_files /index.html =404;
}
```

---

## 八、AI 模型服务部署（可选）

若选择全本地部署，需在服务器上额外启动 vLLM、ASR、TTS。以下给出 Docker 和裸机两种方案。

### 8.1 vLLM（Docker，推荐）

```bash
# 确保模型已下载到 /opt/models/Qwen3___5-0___8B
docker run -d --name vllm --gpus all -p 8000:8000 \
  -v /opt/models:/models \
  vllm/vllm-openai:latest \
  --model /models/Qwen3___5-0___8B \
  --tensor-parallel-size 1 \
  --dtype auto \
  --max-model-len 8192 \
  --gpu-memory-utilization 0.82 \
  --enforce-eager \
  --host 0.0.0.0
```

### 8.2 ASR（Qwen3-ASR）

```bash
# 需先安装 qwen-asr
pip install -U qwen-asr[vllm]

python3 -m qwen_asr.vllm_server \
  --model /opt/models/Qwen3-ASR-0.6B \
  --port 10096 \
  --host 0.0.0.0
```

### 8.3 TTS（CosyVoice）

```bash
git clone --recursive https://github.com/FunAudioLLM/CosyVoice.git ~/CosyVoice
pip install -r ~/CosyVoice/requirements.txt
pip install fastapi uvicorn soundfile

# 启动脚本示例（需根据实际路径调整）
python3 ~/CosyVoice/runtime/python/fastapi/server.py \
  --model_dir /opt/models/CosyVoice-300M \
  --port 50000
```

---

## 九、一键启停脚本

### 9.1 启动所有服务

`/opt/education-agent/scripts/start_all.sh`：

```bash
#!/bin/bash
set -e

echo "Starting EducationAgent services..."
sudo systemctl start postgresql redis-server
sudo systemctl start voice-agent-v2 zcxppt kb-service interface
# sudo systemctl start memory-service auth-service

echo "All services started."
echo "Logs: tail -f /var/log/education-agent/*.log"
```

### 9.2 停止所有服务

`/opt/education-agent/scripts/stop_all.sh`：

```bash
#!/bin/bash
set -e

echo "Stopping EducationAgent services..."
sudo systemctl stop voice-agent-v2 zcxppt kb-service interface
# sudo systemctl stop memory-service auth-service

echo "All services stopped."
```

赋予执行权限：

```bash
chmod +x /opt/education-agent/scripts/start_all.sh
chmod +x /opt/education-agent/scripts/stop_all.sh
```

---

## 十、健康检查与日志

```bash
# 查看各服务日志
tail -f /var/log/education-agent/voice_agent_v2.log
tail -f /var/log/education-agent/zcxppt.log
tail -f /var/log/education-agent/kb-service.log
tail -f /var/log/education-agent/interface.log

# 快速健康检查
curl http://localhost:9000/api/v1/tasks/test/preview   # Voice Agent
curl http://localhost:9200/health                      # KB Service
curl http://localhost:9400/api/v1/ppt/health           # PPT Agent（如有）
curl http://localhost:9500/api/v1/search?q=test       # Interface
```

---

## 十一、生产环境 checklist

- [ ] 修改 `.env` 中所有默认密钥（`JWT_SECRET`、`INTERNAL_KEY`）
- [ ] 为 PostgreSQL 设置强密码，限制监听地址
- [ ] Redis 开启密码认证并绑定内网 IP
- [ ] 配置 HTTPS（Let's Encrypt / 自有证书）
- [ ] vLLM 添加 `--api-key` 防止未授权访问
- [ ] 配置防火墙，仅暴露 80/443，其余端口限制内网访问
- [ ] 开启各服务日志轮转（logrotate）
- [ ] 配置监控告警（Prometheus + Grafana，kb-service 已暴露 `/metrics`）
