"""
通用工具：文件下载、文本分块、UUID 生成
"""
import hashlib
import os
import re
import uuid
from pathlib import Path

import requests

from config import Config


def new_id(prefix: str = "chunk_") -> str:
    """生成带前缀的 UUID"""
    return f"{prefix}{uuid.uuid4().hex[:12]}"


def download_file(url: str, dest_path: str = None) -> str:
    """
    下载远程文件到本地临时目录。
    返回本地文件路径。
    """
    if dest_path is None:
        ext = os.path.splitext(url.split("?")[0].split("/")[-1])[-1] or ".bin"
        dest_path = os.path.join(Config.TMP_DIR, f"{new_id()}{ext}")

    with requests.get(url, timeout=Config.DOWNLOAD_TIMEOUT, stream=True) as r:
        r.raise_for_status()
        total = 0
        max_bytes = Config.MAX_FILE_SIZE * 1024 * 1024
        with open(dest_path, "wb") as f:
            for chunk in r.iter_content(chunk_size=8192):
                total += len(chunk)
                if total > max_bytes:
                    raise ValueError(f"文件超过 {Config.MAX_FILE_SIZE}MB 限制")
                f.write(chunk)

    return dest_path


def extract_text_by_lang(text: str) -> str:
    """按语言比例过滤纯文本中无意义字符（保留中文、英文、数字、常用标点）"""
    text = re.sub(r"[\x00-\x08\x0b\x0c\x0e-\x1f]", "", text)
    text = re.sub(r"[ \t]+", " ", text)
    return text.strip()


def split_into_chunks(text: str, doc_id: str) -> list[dict]:
    """
    按段落优先 + 字符数兜底的分块策略（与 Go 端 splitIntoChunks 行为一致）。
    返回 list[TextChunk]
    """
    if not text:
        return []

    # 先按段落分割
    paras = _split_by_paragraph(text)
    chunks = []
    buf = []
    buf_rune_count = 0
    chunk_idx = 0
    start_char = 0
    prev_content = ""

    def flush():
        nonlocal chunk_idx, start_char, buf_rune_count, buf, prev_content
        content = "".join(buf).strip()
        if _rune_count(content) < Config.MIN_CHUNK_SIZE:
            buf = []
            buf_rune_count = 0
            prev_content = ""
            return

        chunks.append({
            "chunk_id": new_id("chunk_"),
            "doc_id": doc_id,
            "content": content,
            "metadata": {
                "chunk_index": chunk_idx,
                "start_char": start_char,
                "end_char": start_char + len(content),
                "source_type": "text",
            },
        })
        chunk_idx += 1

        # 保留 overlap
        overlap_chars = _last_n_runes(content, Config.CHUNK_OVERLAP)
        start_char += len(content) - len(overlap_chars)
        buf = [overlap_chars]
        buf_rune_count = _rune_count(overlap_chars)
        prev_content = content

    for para in paras:
        para = para.strip()
        if not para:
            continue
        para_runes = _rune_count(para)

        if buf_rune_count + para_runes > Config.CHUNK_SIZE:
            flush()

        if buf:
            buf.append("\n")
            buf_rune_count += 1  # 换行符
        buf.append(para)
        buf_rune_count += para_runes

    flush()
    return chunks


def _split_by_paragraph(text: str) -> list[str]:
    lines = text.split("\n")
    paras, buf = [], []
    for line in lines:
        stripped = line.strip()
        if stripped == "":
            if buf:
                paras.append("".join(buf))
                buf = []
        else:
            buf.append(stripped)
    if buf:
        paras.append("".join(buf))
    return paras


def _rune_count(s: str) -> int:
    return len(s)


def _last_n_runes(s: str, n: int) -> str:
    return "".join(list(s)[-n:]) if n > 0 and s else ""


def cleanup(path: str):
    """删除临时文件"""
    try:
        if path and os.path.exists(path):
            os.remove(path)
    except Exception:
        pass
