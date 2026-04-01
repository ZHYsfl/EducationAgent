"""
分块工具单元测试
"""
import pytest
import sys, os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from utils.common import split_into_chunks, _split_by_paragraph, new_id


class TestSplitByParagraph:
    def test_empty(self):
        assert _split_by_paragraph("") == []

    def test_single_para(self):
        assert _split_by_paragraph("hello world") == ["hello world"]

    def test_multiple_paras(self):
        text = "第一段\n第二行\n\n第二段\n\n\n第三段"
        result = _split_by_paragraph(text)
        assert "第一段 第二行" in result
        assert "第二段" in result
        assert "第三段" in result

    def test_blank_lines_collapsed(self):
        text = "a\n\n\n\nb"
        result = _split_by_paragraph(text)
        assert len(result) == 2


class TestSplitIntoChunks:
    def test_empty(self):
        assert split_into_chunks("", "doc_1") == []

    def test_small_text_no_split(self):
        chunks = split_into_chunks("这是一段较短的文本。" * 10, "doc_1")
        assert len(chunks) == 1
        assert chunks[0]["doc_id"] == "doc_1"
        assert "chunk_id" in chunks[0]

    def test_chunk_has_required_fields(self):
        chunks = split_into_chunks("测试内容。" * 50, "doc_test")
        for c in chunks:
            assert "chunk_id" in c
            assert "doc_id" in c
            assert "content" in c
            assert "metadata" in c
            assert "chunk_index" in c["metadata"]
            assert "start_char" in c["metadata"]
            assert "end_char" in c["metadata"]
            assert "source_type" in c["metadata"]

    def test_sequential_indexes(self):
        chunks = split_into_chunks("段落。" * 200, "doc_seq")
        indexes = [c["metadata"]["chunk_index"] for c in chunks]
        assert indexes == list(range(len(chunks)))

    def test_overlap_present(self):
        """重叠块的内容末尾应该和下一块的开头有重叠"""
        chunks = split_into_chunks("测试文本。" * 300, "doc_ov")
        if len(chunks) >= 2:
            assert chunks[0]["content"][-10:] == chunks[1]["content"][:10]


class TestNewID:
    def test_prefix(self):
        id1 = new_id("chunk_")
        assert id1.startswith("chunk_")
        assert len(id1) == len("chunk_") + 12

    def test_unique(self):
        ids = {new_id() for _ in range(100)}
        assert len(ids) == 100
