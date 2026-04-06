"""
PDF 解析器，基于 PyMuPDF（fitz）
"""
import logging
from typing import Optional

import fitz

from utils.common import new_id, split_into_chunks, cleanup

logger = logging.getLogger(__name__)


def parse_pdf(file_path: str, doc_id: str) -> dict:
    """
    解析 PDF 文件，返回与 Go ParsedDocument 对应的 dict。
    策略：逐页提取文字，保留页码信息。
    """
    doc = fitz.open(file_path)
    total_pages = len(doc)

    full_text_parts = []  # 全量文本，用于后续分块

    for page_num in range(total_pages):
        page = doc[page_num]
        text = page.get_text("text").strip()
        if not text:
            continue

        # 尝试提取章节标题（PDF 大纲书签）
        section_title = ""
        toc = page.get_toc()
        if toc and len(toc) > page_num:
            section_title = toc[page_num][2] if len(toc[page_num]) > 2 else ""

        full_text_parts.append({
            "text": text,
            "page_number": page_num + 1,
            "section_title": section_title,
        })

    doc.close()

    # 全量文本用于分块（保留页码到 metadata）
    all_text = "\n\n".join(p["text"] for p in full_text_parts)
    raw_chunks = split_into_chunks(all_text, doc_id)

    # 补充页码：将 page_number 合并回每个 chunk 的 metadata
    chunks = _attach_page_numbers(raw_chunks, full_text_parts)

    # 提取标题：取第一页前 200 字符作为摘要
    first_text = full_text_parts[0]["text"] if full_text_parts else ""
    title = _extract_title(first_text, total_pages)
    summary = first_text[:200] + "..." if len(first_text) > 200 else first_text

    return {
        "doc_id": doc_id,
        "file_type": "pdf",
        "title": title,
        "text_chunks": chunks,
        "summary": summary,
        "total_pages": total_pages,
    }


def _attach_page_numbers(chunks: list[dict], page_parts: list[dict]) -> list[dict]:
    """根据 chunk 的 start_char / end_char 推算所属页码"""
    if not page_parts:
        return chunks

    # 建立字符偏移到页码的映射
    page_boundaries = []
    offset = 0
    for p in page_parts:
        page_boundaries.append((offset, p["page_number"]))
        offset += len(p["text"]) + 2  # +2 是 \n\n 分隔符

    for chunk in chunks:
        start = chunk["metadata"]["start_char"]
        # 找 start 落在哪一页
        page_num = 1
        for boundary, pnum in page_boundaries:
            if start >= boundary:
                page_num = pnum
        chunk["metadata"]["page_number"] = page_num

    return chunks


def _extract_title(first_text: str, total_pages: int) -> str:
    lines = first_text.split("\n")
    for line in lines[:5]:
        line = line.strip()
        if len(line) >= 5:
            return line[:80] if len(line) > 80 else line
    return f"PDF 文档（共 {total_pages} 页）"
