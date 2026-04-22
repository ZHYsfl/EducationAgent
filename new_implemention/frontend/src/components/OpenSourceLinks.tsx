/**
 * Open-source project, models, dataset, and knowledge base — same pattern as many OSS product UIs.
 */
const RESOURCES: { href: string; title: string; source: 'GitHub' | 'Hugging Face' }[] = [
  {
    href: 'https://github.com/ZHYsfl/EducationAgent',
    title: 'Education Agent · 本项目',
    source: 'GitHub',
  },
  {
    href: 'https://huggingface.co/zengchenxi/interrupt-detection-cot-lora',
    title: '打断检测 LoRA（Qwen3-0.6B）',
    source: 'Hugging Face',
  },
  {
    href: 'https://huggingface.co/ZaneSFL/zh-ppt-voice-agent-model-lora-support-interrupt',
    title: 'Voice Agent LoRA（Qwen3-4B-Instruct）',
    source: 'Hugging Face',
  },
  {
    href: 'https://huggingface.co/datasets/ZaneSFL/zh-ppt-voice-agent-interrupt-dialogues',
    title: '中文语音 PPT 助手 SFT 数据集（6k+ 多轮）',
    source: 'Hugging Face',
  },
  {
    href: 'https://huggingface.co/datasets/allen-1231/Knowledge-Base',
    title: '计算机领域核心知识库',
    source: 'Hugging Face',
  },
]

export function OpenSourceLinks() {
  return (
    <nav className="open-source-links" aria-label="开源与相关资源">
      <div className="open-source-links-header">
        <span className="open-source-links-title">开源与资源</span>
        <span className="open-source-links-hint">模型 · 数据集 · 知识库</span>
      </div>
      <ul className="open-source-list">
        {RESOURCES.map((item) => (
          <li key={item.href}>
            <a
              href={item.href}
              target="_blank"
              rel="noopener noreferrer"
              className="open-source-card"
            >
              <span className={`open-source-badge open-source-badge--${item.source === 'GitHub' ? 'gh' : 'hf'}`}>
                {item.source}
              </span>
              <span className="open-source-card-title">{item.title}</span>
              <span className="open-source-card-arrow" aria-hidden>
                ↗
              </span>
            </a>
          </li>
        ))}
      </ul>
    </nav>
  )
}
