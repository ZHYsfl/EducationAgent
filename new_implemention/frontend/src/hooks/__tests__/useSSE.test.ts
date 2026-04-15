import { renderHook, waitFor } from '@testing-library/react'
import { useSSE } from '../useSSE'
import { vi } from 'vitest'
import * as client from '@/api/client'

describe('useSSE', () => {
  it('starts stream and forwards chunks', async () => {
    const onChunk = vi.fn()
    const { result } = renderHook(() => useSSE({ onChunk }))

    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('data: {"type":"tts","text":"hi"}\n\n'))
        controller.enqueue(new TextEncoder().encode('data: [DONE]\n\n'))
        controller.close()
      },
    })
    vi.spyOn(global, 'fetch').mockResolvedValueOnce({
      ok: true,
      headers: { get: (k: string) => (k === 'Content-Type' ? 'text/event-stream' : null) },
      body: stream,
    } as unknown as Response)

    result.current.start('audio')
    await waitFor(() => expect(onChunk).toHaveBeenCalledWith({ type: 'tts', text: 'hi' }))
  })

  it('aborts running stream', async () => {
    const onChunk = vi.fn()
    const { result } = renderHook(() => useSSE({ onChunk }))

    // Hang forever.
    const stream = new ReadableStream({
      start() {},
    })
    vi.spyOn(global, 'fetch').mockResolvedValueOnce({
      ok: true,
      headers: { get: (k: string) => (k === 'Content-Type' ? 'text/event-stream' : null) },
      body: stream,
    } as unknown as Response)

    result.current.start('audio')
    expect(result.current.isRunning()).toBe(true)
    result.current.abort()
    expect(result.current.isRunning()).toBe(false)
  })
})
