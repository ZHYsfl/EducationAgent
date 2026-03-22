#!/usr/bin/env python3
"""
将 LoRA checkpoint 与基座模型合并，得到完整可部署的模型。

用法:
    python merge_lora.py
    # 默认: checkpoint-25 -> models/sft/interrupt_detector_merged

    python merge_lora.py --checkpoint models/sft/checkpoint-25 --output models/sft/interrupt_detector_merged
"""

import argparse
import torch
from transformers import AutoModelForCausalLM, AutoTokenizer
from peft import PeftModel

BASE_MODEL = "models/Qwen3___5-0___8B"
DEFAULT_CKPT = "models/sft/checkpoint-25"
DEFAULT_OUT = "models/sft/interrupt_detector_merged"


def merge(checkpoint: str, output_dir: str):
    print(f"加载基座模型: {BASE_MODEL}")
    model = AutoModelForCausalLM.from_pretrained(
        BASE_MODEL,
        torch_dtype=torch.bfloat16,
        device_map="auto",
    )
    tokenizer = AutoTokenizer.from_pretrained(BASE_MODEL)

    print(f"加载 LoRA: {checkpoint}")
    model = PeftModel.from_pretrained(model, checkpoint)

    print("合并 LoRA 权重到基座...")
    model = model.merge_and_unload()

    print(f"保存合并后的模型到: {output_dir}")
    model.save_pretrained(output_dir)
    tokenizer.save_pretrained(output_dir)
    print("完成！")


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument("--checkpoint", default=DEFAULT_CKPT, help="LoRA checkpoint 路径")
    p.add_argument("--output", default=DEFAULT_OUT, help="合并后模型输出路径")
    args = p.parse_args()
    merge(args.checkpoint, args.output)
