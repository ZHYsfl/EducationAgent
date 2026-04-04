#!/usr/bin/env python3
"""将 train_data.jsonl 第 1251-2500 行的 conversations 格式转为 content/label/reasoning 格式"""

import json

INPUT_FILE = "data/train_data.jsonl"

def convert_line(obj):
    """将 conversations 格式转为 content/label/reasoning 格式"""
    if "conversations" not in obj:
        return obj  # 已是目标格式，原样返回
    convs = obj["conversations"]
    user_msg = next((c for c in convs if c.get("role") == "user"), None)
    asst_msg = next((c for c in convs if c.get("role") == "assistant"), None)
    if not user_msg or not asst_msg:
        return None
    return {
        "content": user_msg.get("content", ""),
        "label": asst_msg.get("content", ""),
        "reasoning": asst_msg.get("reasoning_content", ""),
    }

def main():
    with open(INPUT_FILE, "r", encoding="utf-8") as f:
        lines = f.readlines()

    # 1-1250 行保持不变，1251-2500 行转换
    result = []
    for i, line in enumerate(lines):
        line_no = i + 1
        line = line.strip()
        if not line:
            result.append("")
            continue
        obj = json.loads(line)
        if 1251 <= line_no <= 2500:
            converted = convert_line(obj)
            if converted:
                result.append(json.dumps(converted, ensure_ascii=False))
            else:
                result.append(line)  # 转换失败则保留原行
        else:
            result.append(line)

    with open(INPUT_FILE, "w", encoding="utf-8") as f:
        f.write("\n".join(result) + "\n")

    print(f"已转换 1251-2500 行，已写回 {INPUT_FILE}")

if __name__ == "__main__":
    main()
