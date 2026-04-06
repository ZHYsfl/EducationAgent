"""
配置管理
"""
import os


class Config:
    # Flask
    HOST = os.getenv("PARSER_HOST", "0.0.0.0")
    PORT = int(os.getenv("PARSER_PORT", "8888"))
    DEBUG = os.getenv("PARSER_DEBUG", "false").lower() == "true"

    # 文件下载
    DOWNLOAD_TIMEOUT = int(os.getenv("PARSER_DOWNLOAD_TIMEOUT", "60"))  # 秒
    MAX_FILE_SIZE = int(os.getenv("PARSER_MAX_FILE_SIZE", "100"))  # MB

    # 临时文件目录
    TMP_DIR = os.getenv("PARSER_TMP_DIR", os.path.join(os.path.dirname(__file__), "tmp"))
    os.makedirs(TMP_DIR, exist_ok=True)

    # 分块参数（与 Go 端 ChunkConfig 保持一致）
    CHUNK_SIZE = int(os.getenv("PARSER_CHUNK_SIZE", "800"))       # 字符数
    CHUNK_OVERLAP = int(os.getenv("PARSER_CHUNK_OVERLAP", "100"))  # 重叠字符数
    MIN_CHUNK_SIZE = int(os.getenv("PARSER_MIN_CHUNK_SIZE", "100"))

    # EasyOCR 语言
    OCR_LANGUAGES = os.getenv("PARSER_OCR_LANGS", "ch_sim,en").split(",")

    # Whisper 模型大小（tiny/base/small/medium/large）
    WHISPER_MODEL = os.getenv("PARSER_WHISPER_MODEL", "base")
