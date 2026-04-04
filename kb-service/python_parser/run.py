#!/usr/bin/env python3
"""
Python 文档解析服务启动脚本
用法：
    python run.py              # 前台运行（开发调试）
    gunicorn -w 2 -b 0.0.0.0:8888 api.server:app   # 生产部署
"""
import sys
import os

# 将项目根目录加入 Python 路径
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from api.server import app
from config import Config

if __name__ == "__main__":
    print(f"[*] Python 解析服务启动中...")
    print(f"    监听地址: {Config.HOST}:{Config.PORT}")
    print(f"    临时目录: {Config.TMP_DIR}")
    print(f"    分块参数: size={Config.CHUNK_SIZE} overlap={Config.CHUNK_OVERLAP}")
    app.run(
        host=Config.HOST,
        port=Config.PORT,
        debug=Config.DEBUG,
    )
