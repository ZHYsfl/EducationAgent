/**
 * Text-to-speech engine with a playback queue.
 *
 * Uses the browser's native speechSynthesis API when available,
 * falling back to a no-op implementation for headless test environments.
 */

export interface TTSOptions {
  rate?: number
  pitch?: number
  lang?: string
}

export type TTSState = 'idle' | 'speaking' | 'paused'

export class TTSEngine {
  private queue: string[] = []
  private state: TTSState = 'idle'
  private options: TTSOptions = { rate: 1, pitch: 1, lang: 'zh-CN' }
  private onStateChange: ((state: TTSState) => void) | null = null
  private onSentenceStart: ((text: string) => void) | null = null
  private onSentenceEnd: ((text: string) => void) | null = null
  private cleared = false

  constructor(options?: TTSOptions) {
    this.options = { ...this.options, ...options }
  }

  setOnStateChange(cb: (state: TTSState) => void) {
    this.onStateChange = cb
  }

  setOnSentenceStart(cb: (text: string) => void) {
    this.onSentenceStart = cb
  }

  setOnSentenceEnd(cb: (text: string) => void) {
    this.onSentenceEnd = cb
  }

  private emitState() {
    this.onStateChange?.(this.state)
  }

  /**
   * Append a sentence to the TTS queue.
   */
  enqueue(text: string) {
    if (!text.trim()) return
    this.cleared = false
    this.queue.push(text.trim())
    if (this.state === 'idle') {
      this.playNext()
    }
  }

  /**
   * Clear the queue and stop any ongoing speech.
   */
  clear() {
    this.queue = []
    this.cleared = true
    if (typeof window !== 'undefined' && window.speechSynthesis) {
      window.speechSynthesis.cancel()
    }
    if (this.state !== 'idle') {
      this.state = 'idle'
      this.emitState()
    }
  }

  /**
   * Pause playback.
   */
  pause() {
    if (typeof window !== 'undefined' && window.speechSynthesis) {
      window.speechSynthesis.pause()
    }
    if (this.state === 'speaking') {
      this.state = 'paused'
      this.emitState()
    }
  }

  /**
   * Resume playback.
   */
  resume() {
    if (typeof window !== 'undefined' && window.speechSynthesis) {
      window.speechSynthesis.resume()
    }
    if (this.state === 'paused') {
      this.state = 'speaking'
      this.emitState()
    }
  }

  /**
   * Whether any audio is currently playing (or paused mid-sentence).
   */
  isActive(): boolean {
    return this.state === 'speaking' || this.state === 'paused'
  }

  private playNext() {
    if (!hasSpeechSynthesis()) {
      // No-op for test environments.
      return
    }
    const text = this.queue.shift()
    if (!text) {
      this.state = 'idle'
      this.emitState()
      return
    }

    this.state = 'speaking'
    this.emitState()
    this.onSentenceStart?.(text)

    const u = new SpeechSynthesisUtterance(text)
    u.rate = this.options.rate ?? 1
    u.pitch = this.options.pitch ?? 1
    u.lang = this.options.lang ?? 'zh-CN'
    u.onend = () => {
      if (!this.cleared) this.onSentenceEnd?.(text)
      this.playNext()
    }
    u.onerror = () => {
      if (!this.cleared) this.onSentenceEnd?.(text)
      this.playNext()
    }
    window.speechSynthesis.speak(u)
  }
}

function hasSpeechSynthesis(): boolean {
  return typeof window !== 'undefined' && !!window.speechSynthesis
}
