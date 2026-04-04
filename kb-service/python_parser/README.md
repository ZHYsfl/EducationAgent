# Python 文档解析服务

为 `kb-service` 提供 PDF / Word / PPTX / 图片 / 视频的专业解析能力。

## 支持格式

| 类型 | 解析方式 | 提取内容 |
|------|---------|---------|
| `pdf` | PyMuPDF | 逐页文字 + 页码 + 书签章节 |
| `docx` | python-docx | 段落文字 + 标题层级 |
| `pptx` | python-pptx | 幻灯片标题 + 正文 + 页码 |
| `image` | EasyOCR | 图片内文字（OCR） |
| `video` | Whisper | 语音转文字（字幕/旁白） |

## 快速开始

### 1. 安装依赖

```bash
cd kb-service/python_parser
pip install -r requirements.txt
```

> **首次运行 OCR/Whisper 时会自动下载模型**，需要联网。EasyOCR 模型约 300MB，Whisper base 模型约 140MB。

### 2. 启动服务

```bash
# 开发调试（前台）
python run.py

# 生产部署
gunicorn -w 2 -b 0.0.0.0:8888 api.server:app
```

### 3. 联调 Go 服务

在 `kb-service` 的 `.env` 或启动参数中配置：

```
PYTHON_PARSER_URL=http://localhost:8888
```

## API 接口

### `POST /parse`

请求：

```json
{
  "file_url": "https://your-oss.com/doc.pdf",
  "file_type": "pdf",
  "doc_id": "doc_abc123"
}
```

响应（与 Go 端 `model.ParsedDocument` 对应）：

```json
{
  "doc_id": "doc_abc123",
  "file_type": "pdf",
  "title": "第一章 Python 基础",
  "text_chunks": [
    {
      "chunk_id": "chunk_7f3a9b2c",
      "doc_id": "doc_abc123",
      "content": "Python 是一种高级编程语言...",
      "metadata": {
        "page_number": 1,
        "section_title": "第一章",
        "chunk_index": 0,
        "start_char": 0,
        "end_char": 150,
        "source_type": "text"
      }
    }
  ],
  "summary": "Python 是一种高级编程语言...",
  "total_pages": 42
}
```

### `GET /health`

返回 `{"status": "ok"}`

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `PARSER_HOST` | `0.0.0.0` | 监听地址 |
| `PARSER_PORT` | `8888` | 监听端口 |
| `PARSER_DEBUG` | `false` | 调试模式 |
| `PARSER_TMP_DIR` | `./tmp` | 临时文件目录 |
| `PARSER_CHUNK_SIZE` | `800` | 分块字符数（与 Go 端一致） |
| `PARSER_CHUNK_OVERLAP` | `100` | 块重叠字符数 |
| `PARSER_MIN_CHUNK_SIZE` | `100` | 最小块字符数 |
| `PARSER_OCR_LANGS` | `ch_sim,en` | OCR 识别语言 |
| `PARSER_WHISPER_MODEL` | `base` | Whisper 模型大小 |
| `PARSER_DOWNLOAD_TIMEOUT` | `60` | 文件下载超时（秒） |
| `PARSER_MAX_FILE_SIZE` | `100` | 文件大小上限（MB） |

## Docker 部署

```dockerfile
FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8888
CMD ["gunicorn", "-w", "2", "-b", "0.0.0.0:8888", "api.server:app"]
```

## 目录结构

```
python_parser/
├── config.py          # 全局配置（环境变量）
├── run.py             # 开发启动入口
├── requirements.txt   # Python 依赖
├── parsers/
│   ├── pdf_parser.py    # PDF 解析
│   ├── docx_parser.py   # Word 解析
│   ├── pptx_parser.py   # PPTX 解析
│   └── media_parser.py  # 图片 OCR + 视频 Whisper
├── utils/
│   └── common.py         # 下载、分块、UUID 等通用工具
└── api/
    └── server.py         # Flask HTTP 服务入口
```
