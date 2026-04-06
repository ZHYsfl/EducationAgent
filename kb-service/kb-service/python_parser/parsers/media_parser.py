"""
图片 OCR 解析器，基于 EasyOCR
视频解析器，基于 OpenAI Whisper
"""
import logging
import os
import tempfile

from utils.common import new_id, split_into_chunks

logger = logging.getLogger(__name__)

# EasyOCR 懒加载（首次调用才初始化，拉取模型需要时间）
_ocr_reader = None


def _get_ocr_reader():
    global _ocr_reader
    if _ocr_reader is None:
        import easyocr
        from config import Config
        logger.info("初始化 EasyOCR 模型，语言=%s", Config.OCR_LANGUAGES)
        _ocr_reader = easyocr.Reader(Config.OCR_LANGUAGES, gpu=True, verbose=False)
    return _ocr_reader


def parse_image(file_path: str, doc_id: str) -> dict:
    """
    解析图片（OCR），返回与 Go ParsedDocument 对应的 dict。
    """
    from PIL import Image
    from config import Config

    img = Image.open(file_path)
    width, height = img.size
    logger.info("OCR 处理图片，尺寸=%dx%d", width, height)

    reader = _get_ocr_reader()
    results = reader.readtext(file_path, detail=1)

    lines = []
    for bbox, text, confidence in results:
        if confidence < 0.5:
            continue
        text = text.strip()
        if text:
            lines.append(text)

    full_text = "\n".join(lines)
    chunks = split_into_chunks(full_text, doc_id)

    # OCR 结果没有语义页码，统一标记为 1
    for chunk in chunks:
        chunk["metadata"]["page_number"] = 1
        chunk["metadata"]["source_type"] = "ocr"

    # 图片宽高信息附加到元数据
    for chunk in chunks:
        chunk["metadata"]["image_url"] = f"file://{file_path}"

    summary = full_text[:200] + "..." if len(full_text) > 200 else full_text
    title = f"图片 OCR 结果 ({width}x{height})"

    return {
        "doc_id": doc_id,
        "file_type": "image",
        "title": title,
        "text_chunks": chunks,
        "summary": summary,
    }


# Whisper 懒加载
_whisper_model = None


def _get_whisper_model():
    global _whisper_model
    if _whisper_model is None:
        import whisper
        from config import Config
        logger.info("加载 Whisper 模型: %s", Config.WHISPER_MODEL)
        _whisper_model = whisper.load_model(Config.WHISPER_MODEL)
    return _whisper_model


def parse_video(file_path: str, doc_id: str) -> dict:
    """
    解析视频文件：提取音频 → Whisper 语音识别 → 文本分块。
    返回与 Go ParsedDocument 对应的 dict。
    """
    import whisper
    from moviepy import AudioFileClip
    from config import Config

    logger.info("开始处理视频: %s", file_path)

    # 从视频提取音频（moviepy）
    audio_path = os.path.join(Config.TMP_DIR, f"{new_id()}_audio.wav")
    try:
        clip = AudioFileClip(file_path)
        clip.write_audiofile(audio_path, fps=16000, verbose=False, logger=None)
        clip.close()
    except Exception as e:
        raise RuntimeError(f"视频音频提取失败: {e}")

    try:
        # Whisper 语音识别
        model = _get_whisper_model()
        logger.info("Whisper 开始转写，模型=%s", Config.WHISPER_MODEL)
        result = model.transcribe(audio_path, language="zh", verbose=False)
        full_text = result["text"].strip()
    finally:
        if os.path.exists(audio_path):
            os.remove(audio_path)

    chunks = split_into_chunks(full_text, doc_id)
    for chunk in chunks:
        chunk["metadata"]["source_type"] = "video_transcript"

    summary = full_text[:200] + "..." if len(full_text) > 200 else full_text
    title = os.path.basename(file_path)

    return {
        "doc_id": doc_id,
        "file_type": "video",
        "title": title,
        "text_chunks": chunks,
        "summary": summary,
    }
