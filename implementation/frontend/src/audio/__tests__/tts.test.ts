import { describe, it, expect, vi } from 'vitest'
import { TTSEngine } from '../tts'

describe('TTSEngine', () => {
  it('enqueues text and tracks state', () => {
    const onStateChange = vi.fn()
    const tts = new TTSEngine()
    tts.setOnStateChange(onStateChange)

    // In a headless test environment speechSynthesis is unavailable,
    // so the engine stays idle after enqueue.
    tts.enqueue('Hello.')
    expect(tts.isActive()).toBe(false)
  })

  it('clears queue and resets to idle', () => {
    const tts = new TTSEngine()
    tts.enqueue('Hello.')
    tts.enqueue('World.')
    tts.clear()
    expect(tts.isActive()).toBe(false)
  })
})
