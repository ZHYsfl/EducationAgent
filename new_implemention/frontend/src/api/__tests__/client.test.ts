import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  startConversation,
  updateRequirements,
  requireConfirm,
  sendToPPTAgent,
  fetchFromPPTMessageQueue,
  vadStart,
  vadEnd,
  isIgnoredResponse,
} from '../client'

const mockFetch = vi.fn()
global.fetch = mockFetch

describe('API client', () => {
  beforeEach(() => {
    mockFetch.mockReset()
  })

  it('startConversation posts correct body', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: null }),
    })
    const res = await startConversation()
    expect(res.code).toBe(200)
    expect(mockFetch).toHaveBeenCalledWith('/api/v1/start_conversation', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ from: 'frontend', to: 'voice_agent' }),
    }))
  })

  it('updateRequirements sends partial requirements', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: { missing_fields: ['audience'] } }),
    })
    const res = await updateRequirements({ topic: 'math' })
    expect(res.data?.missing_fields).toEqual(['audience'])
  })

  it('requireConfirm sends full requirements', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: null }),
    })
    const res = await requireConfirm({
      topic: 'math',
      style: 'simple',
      total_pages: 10,
      audience: 'kids',
    })
    expect(res.code).toBe(200)
  })

  it('sendToPPTAgent sends data string', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: null }),
    })
    const res = await sendToPPTAgent('feedback')
    expect(res.code).toBe(200)
  })

  it('fetchFromPPTMessageQueue GETs and returns string data', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: 'hello' }),
    })
    const res = await fetchFromPPTMessageQueue()
    expect(res.data).toBe('hello')
    expect(mockFetch).toHaveBeenCalledWith('/api/v1/fetch_from_ppt_message_queue')
  })

  it('vadStart posts audio payload', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: { interrupt: true } }),
    })
    const res = await vadStart({ audio: 'base64', format: 'pcm' })
    expect(res.data?.interrupt).toBe(true)
  })
})

describe('vadEnd SSE streaming', () => {
  function createReadableStream(chunks: string[]) {
    return new ReadableStream({
      start(controller) {
        for (const c of chunks) {
          controller.enqueue(new TextEncoder().encode(c))
        }
        controller.close()
      },
    })
  }

  it('parses SSE chunks and calls onChunk', async () => {
    const stream = createReadableStream([
      'data: {"type":"tts","text":"hello"}\n\n',
      'data: {"type":"turn_end"}\n\n',
      'data: [DONE]\n\n',
    ])
    mockFetch.mockResolvedValueOnce({
      ok: true,
      headers: { get: (k: string) => (k === 'Content-Type' ? 'text/event-stream' : null) },
      body: stream,
    } as unknown as Response)

    const chunks: unknown[] = []
    await vadEnd({ audio: 'base64', format: 'pcm' }, (c) => chunks.push(c))

    expect(chunks).toHaveLength(2)
    expect(chunks[0]).toEqual({ type: 'tts', text: 'hello' })
    expect(chunks[1]).toEqual({ type: 'turn_end' })
  })

  it('aborts when signal is triggered', async () => {
    const controller = new AbortController()
    const stream = createReadableStream(['data: {"type":"tts","text":"a"}\n\n'])
    mockFetch.mockResolvedValueOnce({
      ok: true,
      headers: { get: (k: string) => (k === 'Content-Type' ? 'text/event-stream' : null) },
      body: stream,
    } as unknown as Response)

    const promise = vadEnd({ audio: 'base64', format: 'pcm' }, () => {}, controller.signal)
    controller.abort('test-abort')
    await expect(promise).rejects.toEqual('test-abort')
  })
})

describe('isIgnoredResponse', () => {
  it('returns true for ignored object', () => {
    expect(isIgnoredResponse({ ignored: true })).toBe(true)
  })

  it('returns false for null', () => {
    expect(isIgnoredResponse(null)).toBe(false)
  })

  it('returns false for string', () => {
    expect(isIgnoredResponse('hello')).toBe(false)
  })
})
