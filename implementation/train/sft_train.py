"""
SFT Training Script for Voice Agent (Unsloth + QLoRA)
======================================================
Environment: single RTX 4090 24 GB
Base Model : Qwen/Qwen3-4B-Instruct-2507 (local snapshot under MODEL_NAME; use this Hub id in README / adapter_config when publishing)
Dataset    : final.jsonl (OpenAI-style messages, one sample per line)

Install dependencies before running:
    pip install unsloth trl datasets transformers accelerate
"""

# screen -dmS train bash -c 'source /root/.venv/bin/activate && cd /root/autodl-tmp && exec python -u train.py > /root/autodl-tmp/log.txt 2>&1'

import os

# 仅使用本地文件，避免从 Hugging Face 拉取（须在 import transformers/unsloth 之前）
os.environ.setdefault("HF_HUB_OFFLINE", "1")
os.environ.setdefault("TRANSFORMERS_OFFLINE", "1")
# 长时挂机：降低显存碎片导致的偶发 OOM（需 PyTorch 2.0+）
os.environ.setdefault("PYTORCH_CUDA_ALLOC_CONF", "expandable_segments:True")

import unsloth  # noqa: F401 — 必须在 transformers / trl / peft 之前，以启用 unsloth 优化

import json
import torch
from datasets import Dataset
from transformers import TrainingArguments
from transformers.trainer_utils import get_last_checkpoint
from trl import SFTTrainer
from unsloth import FastLanguageModel

# ===================== 路径（数据盘，本地已有模型与数据） =====================
_ROOT = "/root/autodl-tmp"
# 本地底座快照目录（与 Hub 上 Qwen/Qwen3-4B-Instruct-2507 对应；save_model 会写入该路径到 adapter_config，发 HF 前请改成 Hub id）
MODEL_NAME = os.path.join(_ROOT, "train")
DATA_PATH = os.path.join(_ROOT, "data", "final.jsonl")
OUTPUT_DIR = os.path.join(_ROOT, "output")
# ============================================================

# ---------- 挂机稳态优先（显存留余量 + 控磁盘） ----------
# 2048 时约 23GB+ 显存偏满；降到 1536 可明显降压，降低夜间 OOM/碎片风险
MAX_SEQ_LENGTH = 1536
DTYPE = torch.bfloat16              # Ampere 架构原生支持，训练更稳
LOAD_IN_4BIT = True                 # QLoRA：4-bit 量化底座 + LoRA

# LoRA 略减小 rank，进一步省显存与优化器状态，仍足够 SFT
LORA_R = 8
LORA_ALPHA = 16                     # 保持 alpha=2r 的常见比例
LORA_DROPOUT = 0                    # 0 即可，unsloth 对 dropout 优化有限
TARGET_MODULES = [
    "q_proj", "k_proj", "v_proj", "o_proj",
    "gate_proj", "up_proj", "down_proj",
]

# Trainer 参数
BATCH_SIZE = 1
GRADIENT_ACCUMULATION_STEPS = 8     # 有效 batch = 8，稳定更新
NUM_EPOCHS = 3
LEARNING_RATE = 2e-4                # LoRA 保守学习率
WARMUP_RATIO = 0.1
WEIGHT_DECAY = 0.01
MAX_GRAD_NORM = 0.3
SEED = 42

# 密集保存：方便按 loss 曲线挑模型（4880 条 / 8 ≈ 610 steps/epoch，3 epoch ≈ 1830 steps）
SAVE_STEPS = 50                     # 每 50 steps 存一个 checkpoint
SAVE_TOTAL_LIMIT = 0                # 0 = 不限制，保留全部供挑选；磁盘紧张可改为 30~40
LOGGING_STEPS = 5                   # 每 5 steps 打一次 loss，曲线更细腻
# ---------------------------------------------------


def load_jsonl_dataset(path: str, tokenizer) -> Dataset:
    """
    读取 final.jsonl，每行是一个 JSON。
    支持两种外层格式：
      - list: [{"role": "system", ...}, ...]
      - dict: {"messages": [{"role": "system", ...}, ...]}
    使用 tokenizer.chat_template 拼接成单条 text 供 SFTTrainer 使用。
    """
    samples = []
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            obj = json.loads(line)
            messages = obj if isinstance(obj, list) else obj.get("messages", [])

            # Qwen Instruct 系列支持 system/user/assistant/tool 全角色
            text = tokenizer.apply_chat_template(
                messages,
                tokenize=False,
                add_generation_prompt=False,
            )
            samples.append({"text": text})

    return Dataset.from_list(samples)


def main():
    assert os.path.isdir(MODEL_NAME), f"本地模型目录不存在: {MODEL_NAME}"
    assert os.path.isfile(DATA_PATH), f"训练数据不存在: {DATA_PATH}"

    os.makedirs(OUTPUT_DIR, exist_ok=True)

    # 1) 加载底座模型 + Tokenizer（仅本地；local_files_only 禁止联网解析/下载）
    model, tokenizer = FastLanguageModel.from_pretrained(
        model_name=MODEL_NAME,
        max_seq_length=MAX_SEQ_LENGTH,
        dtype=DTYPE,
        load_in_4bit=LOAD_IN_4BIT,
        local_files_only=True,
    )

    # 2) 附加 LoRA（保守标准配置）
    model = FastLanguageModel.get_peft_model(
        model,
        r=LORA_R,
        target_modules=TARGET_MODULES,
        lora_alpha=LORA_ALPHA,
        lora_dropout=LORA_DROPOUT,
        bias="none",
        use_gradient_checkpointing="unsloth",  # unsloth 专用 checkpoint，省显存且不掉速
        random_state=SEED,
        use_rslora=False,
    )

    # 3) 加载并格式化数据集
    dataset = load_jsonl_dataset(DATA_PATH, tokenizer)
    print(f"Dataset loaded: {len(dataset)} samples")

    # 4) 训练参数 —— 保守稳定优先
    training_args = TrainingArguments(
        output_dir=OUTPUT_DIR,
        num_train_epochs=NUM_EPOCHS,
        per_device_train_batch_size=BATCH_SIZE,
        gradient_accumulation_steps=GRADIENT_ACCUMULATION_STEPS,
        learning_rate=LEARNING_RATE,
        warmup_ratio=WARMUP_RATIO,
        weight_decay=WEIGHT_DECAY,
        max_grad_norm=MAX_GRAD_NORM,
        lr_scheduler_type="cosine",
        logging_steps=LOGGING_STEPS,
        save_strategy="steps",
        save_steps=SAVE_STEPS,
        save_total_limit=SAVE_TOTAL_LIMIT,
        seed=SEED,
        report_to="tensorboard",   # 启用 TensorBoard 日志，方便可视化 loss 曲线
        logging_dir=os.path.join(OUTPUT_DIR, "logs"),
        logging_first_step=True,
        remove_unused_columns=False,
        dataloader_num_workers=0,  # 避免 DataLoader worker 崩溃，单卡最稳
        # bf16/fp16 由 unsloth 内部控制，TrainingArguments 里不额外指定避免冲突
    )

    # 5) 初始化 SFTTrainer（unsloth 会注入显存优化 patch）
    trainer = SFTTrainer(
        model=model,
        tokenizer=tokenizer,
        train_dataset=dataset,
        dataset_text_field="text",
        max_seq_length=MAX_SEQ_LENGTH,
        packing=False,              # False 更稳，避免长样本打包导致 OOM
        args=training_args,
    )

    # 6) 自动断点续训：检测 output_dir 中最后一个 checkpoint
    last_checkpoint = get_last_checkpoint(OUTPUT_DIR)
    if last_checkpoint is not None:
        print(f"\n>>> 检测到断点，自动恢复训练: {last_checkpoint}\n")
        trainer.train(resume_from_checkpoint=last_checkpoint)
    else:
        print("\n>>> 从头开始训练\n")
        trainer.train()

    # 7) 保存最终 LoRA 权重 + Tokenizer
    final_path = os.path.join(OUTPUT_DIR, "final")
    trainer.save_model(final_path)
    tokenizer.save_pretrained(final_path)
    print(f"Training complete. Model saved to: {final_path}")


if __name__ == "__main__":
    main()
