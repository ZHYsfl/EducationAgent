import { renderHook, waitFor, act } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { useConversation } from '../useConversation'
import * as client from '@/api/client'
import { useConversationStore } from '@/store/conversationStore'

describe('useConversation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useConversationStore.setState({
      status: 'idle',
      history: [],
      assistantBuffer: '',
      hasEnteredActionPhase: false,
      spokenText: '',
      ttsPendingText: '',
      isInterrupted: false,
      confirmPayload: null,
      pptMessages: [],
    })

    navigator.mediaDevices.getUserMedia = vi.fn().mockResolvedValue({
      getTracks: () => [{ stop: vi.fn() }],
    })
  })

  it('starts conversation and sets listening status', async () => {
    vi.spyOn(client, 'startConversation').mockResolvedValue({
      code: 200,
      message: 'success',
      data: null,
    } as Awaited<ReturnType<typeof client.startConversation>>)

    const { result } = renderHook(() => useConversation())
    await act(async () => {
      await result.current.start()
    })

    await waitFor(() => expect(result.current.status).toBe('listening'))
  })

  it('stops conversation and resets to idle', async () => {
    vi.spyOn(client, 'startConversation').mockResolvedValue({
      code: 200,
      message: 'success',
      data: null,
    } as Awaited<ReturnType<typeof client.startConversation>>)

    const { result } = renderHook(() => useConversation())
    await act(async () => {
      await result.current.start()
    })
    await waitFor(() => expect(result.current.status).toBe('listening'))

    act(() => {
      result.current.stop()
    })
    await waitFor(() => expect(result.current.status).toBe('idle'))
  })
})
