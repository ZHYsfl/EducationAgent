#!/usr/bin/env python3
"""用合并后的打断检测模型做推理测试"""

import re
import torch
from transformers import AutoModelForCausalLM, AutoTokenizer

MODEL_PATH = "models/sft/interrupt_detector_merged"
INSTRUCTION = "你是打断检测器。请判断内容是否应该被打断。请先在<think></think>中给出理由，然后输出最终标签，只能是 interrupt 或 do not interrupt。"
DEBUG = True  # 打印原始输出便于排查


def strip_think(text: str) -> str:
    """去掉 <think>...</think> 及其内容，保留后面的标签"""
    text = re.sub(r"<think>.*?</think>", "", text, flags=re.DOTALL)
    return text.strip()


def predict(model, tokenizer, text: str):
    """返回 (reasoning, label)"""
    messages = [
        {"role": "system", "content": INSTRUCTION},
        {"role": "user", "content": text},
    ]
    prompt = tokenizer.apply_chat_template(
        messages,
        tokenize=False,
        add_generation_prompt=True,
        enable_thinking=True,
    )
    # 若模板未插入 thinking 开头，手动补上（确保模型输出 <think>...</think>）
    if "<think>" not in prompt:
        prompt = prompt.rstrip() + "<think>\n"
    elif prompt.rstrip().endswith("<think>") or prompt.rstrip().endswith("<think>\n"):
        pass  # 已有正确开头
    else:
        # 若模板插入了 <think>\n\n</think>，说明 enable_thinking 未生效，改为手动构造
        if "<think>\n\n</think>" in prompt:
            prompt = prompt.replace("<think>\n\n</think>\n\n", "<think>\n")
    if DEBUG:
        print(f"  [prompt tail] ...{repr(prompt[-120:])}")
    inputs = tokenizer(prompt, return_tensors="pt").to(model.device)
    out = model.generate(
        **inputs,
        max_new_tokens=256,
        do_sample=False,
        pad_token_id=tokenizer.pad_token_id,
    )
    reply = tokenizer.decode(out[0][inputs["input_ids"].shape[1] :], skip_special_tokens=True)
    if DEBUG:
        print(f"  [raw] {reply!r}")
    # 解析 <think>...</think> 和 label
    think_match = re.search(r"<think>(.*?)</think>", reply, re.DOTALL)
    reasoning = think_match.group(1).strip() if think_match else ""
    label_part = strip_think(reply).strip()
    # 优先匹配 "do not interrupt"
    if "do not interrupt" in label_part.lower() or "do-not-interrupt" in label_part.lower():
        label = "do not interrupt"
    elif "interrupt" in label_part.lower():
        label = "interrupt"
    else:
        label = "do not interrupt"  # 未知输出保守处理
    return reasoning, label


def main():
    print(f"加载模型: {MODEL_PATH}")
    tokenizer = AutoTokenizer.from_pretrained(MODEL_PATH)
    tokenizer.pad_token = tokenizer.eos_token
    model = AutoModelForCausalLM.from_pretrained(
        MODEL_PATH,
        torch_dtype=torch.bfloat16,
        device_map="auto",
    )
    model.eval()

    tests = [
        "嗯嗯",
        "啊啊呵呵",
        "帮我查一下",
        "这个月的账单怎么用",
        "啊啃啊",
        "想问一下",
    ]
    print("\n--- 推理测试 ---\n")
    for t in tests:
        reasoning, label = predict(model, tokenizer, t)
        print(f"输入: {t!r}")
        print(f"  理由: {reasoning[:80]}{'...' if len(reasoning) > 80 else ''}")
        print(f"  标签: {label}\n")


if __name__ == "__main__":
    main()
