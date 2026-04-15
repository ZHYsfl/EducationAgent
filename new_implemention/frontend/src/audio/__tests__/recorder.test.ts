import { describe, it, expect, vi } from 'vitest'
import { AudioRecorder } from '../recorder'

describe('AudioRecorder', () => {
  it('starts and stops recording', async () => {
    const recorder = new AudioRecorder()
    const getUserMediaMock = vi.fn().mockResolvedValue({
      getTracks: () => [{ stop: vi.fn() }],
    })
    navigator.mediaDevices.getUserMedia = getUserMediaMock

    await recorder.start()
    expect(recorder.isRecording()).toBe(true)

    const segment = recorder.stop()
    expect(recorder.isRecording()).toBe(false)
    // No audio captured in the mocked environment.
    expect(segment).toBeNull()
  })
})
