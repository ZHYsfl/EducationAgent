import os
os.environ.setdefault("WANDB_PROJECT", "1")
os.environ.setdefault("HF_HUB_OFFLINE", "1")
os.environ.setdefault("TRANSFORMERS_OFFLINE", "1")

import torch
from datasets import load_dataset
from transformers import AutoModelForCausalLM, TrainingArguments, AutoTokenizer, Trainer, EarlyStoppingCallback
from peft import LoraConfig, get_peft_model

MODEL_PATH = "models/Qwen3___5-0___8B"
TRAIN_FILE = "data/train_data.jsonl"
MAX_SEQ_LENGTH = 512
INSTRUCTION = "你是打断检测器。请判断内容是否应该被打断。请先在<think></think>中给出理由，然后输出最终标签，只能是 interrupt 或 do not interrupt。"
LORA_RANK = 64
OUTPUT_DIR = "models/sft/"

def train_sft():
    if not torch.cuda.is_available():
        raise RuntimeError(
            "CUDA 未检测到！请确认：\n"
            "1. 已安装带 CUDA 的 PyTorch: pip install torch --index-url https://download.pytorch.org/whl/cu124\n"
            "2. nvidia-smi 能正常显示 GPU"
        )
    print(f"使用 GPU: {torch.cuda.get_device_name(0)}")

    tokenizer = AutoTokenizer.from_pretrained(MODEL_PATH)
    tokenizer.pad_token = tokenizer.eos_token
    
    ds = load_dataset("json", data_files={"train": TRAIN_FILE})
    split = ds["train"].train_test_split(test_size=0.1, seed=42)
    dataset = {"train": split["train"], "dev": split["test"]}

    def tokenize(examples):
        texts = []
        contents = examples.get("content", [])
        labels = examples.get("label", [])
        reasonings = examples.get("reasoning", [])
        for content, label, reasoning in zip(contents, labels, reasonings):
            out = f"<think>\n{reasoning}\n</think>\n{label}"
            text = tokenizer.apply_chat_template([
                {"role": "system", "content": INSTRUCTION.strip()},
                {"role": "user", "content": (content or "").strip()},
                {"role": "assistant", "content": out.strip()},
            ], tokenize=False)
            texts.append(text)
        
        enc = tokenizer(texts, truncation=True, max_length=MAX_SEQ_LENGTH, padding="max_length")
        enc["labels"] = [ids.copy() for ids in enc["input_ids"]]
        return enc
    
    train_ds = dataset["train"].map(tokenize, batched=True, remove_columns=dataset["train"].column_names)
    eval_ds = dataset["dev"].map(tokenize, batched=True, remove_columns=dataset["dev"].column_names)
    
    model = AutoModelForCausalLM.from_pretrained(
        MODEL_PATH,
        torch_dtype=torch.bfloat16,
        device_map="auto",
    )
    model.gradient_checkpointing_enable()
    model = get_peft_model(model, LoraConfig(
        r=LORA_RANK, lora_alpha=LORA_RANK,
        target_modules=["q_proj","k_proj","v_proj","o_proj","gate_proj","up_proj","down_proj"],
        lora_dropout=0.05, bias="none", task_type="CAUSAL_LM"
    ))
    model.print_trainable_parameters()
    
    trainer = Trainer(
        model=model,
        args=TrainingArguments(
            output_dir=OUTPUT_DIR,
            per_device_train_batch_size=1,
            gradient_accumulation_steps=8,
            num_train_epochs=2,
            max_steps=300,
            learning_rate=1e-4,
            bf16=True,
            logging_steps=10,
            eval_strategy="steps",
            eval_steps=25,
            save_strategy="steps",
            save_steps=25,
            save_total_limit=2,
            load_best_model_at_end=True,
            metric_for_best_model="eval_loss",
            greater_is_better=False,
            report_to="wandb",
            dataloader_num_workers=0,
        ),
        train_dataset=train_ds,
        eval_dataset=eval_ds,
        processing_class=tokenizer,
        callbacks=[EarlyStoppingCallback(early_stopping_patience=3)],
    )
    
    trainer.train()
    model.save_pretrained(f"{OUTPUT_DIR}/final_model")
    tokenizer.save_pretrained(f"{OUTPUT_DIR}/final_model")
    print("Done")

if __name__ == "__main__":
    train_sft()