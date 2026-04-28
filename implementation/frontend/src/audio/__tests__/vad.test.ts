import { describe, it, expect, vi } from 'vitest'
import { VADetector } from '../vad'

describe('VADetector', () => {
  it('starts without error', async () => {
    const onSpeechStart = vi.fn()
    const onSpeechEnd = vi.fn()
    const getUserMediaMock = vi.fn().mockResolvedValue({
      getTracks: () => [{ stop: vi.fn() }],
    })
    navigator.mediaDevices.getUserMedia = getUserMediaMock

    const vad = new VADetector(
      { thresholdDb: -40, minSpeechDurationMs: 100, minSilenceDurationMs: 300 },
      { onSpeechStart, onSpeechEnd },
    )

    await vad.start()
    // No speech in the mocked analyser data.
    expect(onSpeechStart).not.toHaveBeenCalled()

    vad.stop()
  })
})
