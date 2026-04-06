"""
Word DOCX 解析器，基于 python-docx
"""
import logging
from docx import Document

from utils.common import split_into_chunks

logger = logging.getLogger(__name__)


def parse_docx(file_path: str, doc_id: str) -> dict:
    """
    解析 DOCX 文件，返回与 Go ParsedDocument 对应的 dict。
    策略：按段落提取，识别标题层级。
    """
    doc = Document(file_path)

    paragraphs = []
    full_text_parts = []

    for para in doc.paragraphs:
        text = para.text.strip()
        if not text:
            continue

        # 判断是否为标题（Microsoft Word 标题样式通常以 Heading 开头）
        style_name = para.style.name if para.style else "Normal"
        is_heading = style_name.startswith("Heading") or style_name.startswith("Title")

        paragraphs.append({"text": text, "is_heading": is_heading, "style": style_name})
        full_text_parts.append(text)

    # 构建带结构信息的完整文本
    structured_text = _build_structured_text(paragraphs)
    raw_chunks = split_into_chunks(structured_text, doc_id)
    chunks = _attach_section_titles(raw_chunks, paragraphs)

    # 提取标题：取第一个 Heading 段落
    title = _extract_title(paragraphs)
    summary = _build_summary(full_text_parts)

    return {
        "doc_id": doc_id,
        "file_type": "docx",
        "title": title,
        "text_chunks": chunks,
        "summary": summary,
    }


def _build_structured_text(paragraphs: list[dict]) -> str:
    """将段落列表拼接为带结构的文本（标题独占一行）"""
    lines = []
    for p in paragraphs:
        if p["is_heading"]:
            lines.append(f"\n## {p['text']}\n")
        else:
            lines.append(p["text"])
    return "\n".join(lines)


def _attach_section_titles(chunks: list[dict], paragraphs: list[dict]) -> list[dict]:
    """为每个 chunk 补充所属章节标题"""
    if not paragraphs:
        return chunks

    # 建立段落文本到章节标题的映射
    section_map = {}
    current_section = ""
    for p in paragraphs:
        if p["is_heading"]:
            current_section = p["text"]
        else:
            section_map[p["text"][:50]] = current_section  # 用前50字符粗略匹配

    for chunk in chunks:
        chunk_text_preview = chunk["content"][:50]
        chunk["metadata"]["section_title"] = section_map.get(chunk_text_preview, "")

    return chunks


def _extract_title(paragraphs: list[dict]) -> str:
    for p in paragraphs:
        if p["is_heading"]:
            return p["text"][:80]
    first = paragraphs[0]["text"] if paragraphs else "Word 文档"
    return first[:80]


def _build_summary(full_text_parts: list[str]) -> str:
    combined = "\n".join(full_text_parts)
    return combined[:200] + "..." if len(combined) > 200 else combined
