import { describe, it, expect, vi, beforeEach } from 'vitest'
import {
  startConversation,
  sendToArmAgent,
  getMessageFromArmAgent,
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

  it('sendToArmAgent posts content to the arm agent', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: '发送成功' }),
    })
    const res = await sendToArmAgent('把红色方块放到左边')
    expect(res.data).toBe('发送成功')
    expect(mockFetch).toHaveBeenCalledWith('/api/v1/send_to_arm_agent', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ from: 'frontend', to: 'arm_agent', content: '把红色方块放到左边' }),
    }))
  })

  it('getMessageFromArmAgent POSTs empty body and returns string data', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: 'all_messages_from_arm_agent:消息1;消息2' }),
    })
    const res = await getMessageFromArmAgent()
    expect(res.data).toBe('all_messages_from_arm_agent:消息1;消息2')
    expect(mockFetch).toHaveBeenCalledWith('/api/v1/get_message_from_arm_agent', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({}),
    }))
  })

  it('getMessageFromArmAgent handles no-new-message response', async () => {
    mockFetch.mockResolvedValueOnce({
      json: async () => ({ code: 200, message: 'success', data: '当前没有新消息' }),
    })
    const res = await getMessageFromArmAgent()
    expect(res.data).toBe('当前没有新消息')
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
