"""
Flask API 入口
GET  /health          → {"status": "ok"}
POST /parse            → ParsedDocument（与 Go 端 model.ParsedDocument 对应）
"""
import logging
import os
import time

from flask import Flask, request, jsonify

from config import Config
from parsers import parse_pdf, parse_docx, parse_pptx, parse_image, parse_video
from utils import download_file, cleanup

# 配置日志
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s: %(message)s",
)
logger = logging.getLogger("parser_api")

app = Flask(__name__)


# ── 健康检查 ──────────────────────────────────────────────────────────────────

@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "ok"})


# ── 解析接口 ──────────────────────────────────────────────────────────────────

@app.route("/parse", methods=["POST"])
def parse():
    """
    请求体（与 Go 端 model.ParseInput 对应）：
    {
        "file_url": "https://...",
        "file_type": "pdf",       // pdf | docx | pptx | image | video
        "doc_id": "doc_xxxx",
        "content": ""             // web_snippet 场景的内容，此处忽略
    }

    响应体（与 Go 端 model.ParsedDocument 对应）：
    {
        "doc_id": "...",
        "file_type": "...",
        "title": "...",
        "text_chunks": [...],
        "summary": "...",
        "total_pages": N          // 仅 PDF/PPTX 有
    }
    """
    start = time.time()
    body = request.get_json(force=True)

    file_url = body.get("file_url", "")
    file_type = body.get("file_type", "")
    doc_id = body.get("doc_id", "")

    if not file_url:
        return jsonify({"error": "file_url is required"}), 400
    if not file_type:
        return jsonify({"error": "file_type is required"}), 400
    if not doc_id:
        return jsonify({"error": "doc_id is required"}), 400

    # 分发到对应解析器
    parser_map = {
        "pdf": parse_pdf,
        "docx": parse_docx,
        "pptx": parse_pptx,
        "image": parse_image,
        "video": parse_video,
    }

    parser = parser_map.get(file_type)
    if not parser:
        return jsonify({"error": f"unsupported file_type: {file_type}"}), 400

    local_path = None
    try:
        logger.info("[%s] 开始解析 doc_id=%s, url=%s", file_type, doc_id, file_url)
        local_path = download_file(file_url)
        result = parser(local_path, doc_id)
        elapsed = time.time() - start
        logger.info("[%s] 解析完成 doc_id=%s, chunks=%d, elapsed=%.2fs",
                     file_type, doc_id, len(result["text_chunks"]), elapsed)
        return jsonify(result)

    except Exception as e:
        logger.exception("[%s] 解析失败 doc_id=%s: %s", file_type, doc_id, e)
        return jsonify({"error": str(e)}), 500

    finally:
        if local_path:
            cleanup(local_path)


if __name__ == "__main__":
    app.run(
        host=Config.HOST,
        port=Config.PORT,
        debug=Config.DEBUG,
    )
