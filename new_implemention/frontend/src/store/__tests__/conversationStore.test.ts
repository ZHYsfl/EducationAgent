import { describe, it, expect, beforeEach } from 'vitest'
import { useConversationStore } from '../conversationStore'

describe('conversationStore', () => {
  beforeEach(() => {
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
  })

  it('updates status', () => {
    useConversationStore.getState().setStatus('speaking')
    expect(useConversationStore.getState().status).toBe('speaking')
  })

  it('appends history', () => {
    useConversationStore.getState().appendHistory({ role: 'user', content: 'hi' })
    expect(useConversationStore.getState().history).toHaveLength(1)
  })

  it('replaces last assistant message', () => {
    useConversationStore.getState().appendHistory({ role: 'assistant', content: 'old' })
    useConversationStore.getState().appendHistory({ role: 'user', content: 'hi' })
    useConversationStore.getState().replaceLastAssistant('new')
    const msgs = useConversationStore.getState().history
    expect(msgs[0].content).toBe('new')
    expect(msgs[1].content).toBe('hi')
  })

  it('handles tts chunk', () => {
    useConversationStore.getState().resetBuffer()
    useConversationStore.getState().handleSSEChunk({ type: 'tts', text: 'hello' })
    expect(useConversationStore.getState().assistantBuffer).toBe('hello')
    expect(useConversationStore.getState().ttsPendingText).toBe('hello')
    expect(useConversationStore.getState().status).toBe('speaking')
  })

  it('handles action + tool + turn_end', () => {
    useConversationStore.getState().resetBuffer()
    useConversationStore.getState().handleSSEChunk({ type: 'action', payload: 'require_confirm' })
    useConversationStore.getState().handleSSEChunk({ type: 'tool', text: '<tool>ok</tool>' })
    useConversationStore.getState().handleSSEChunk({ type: 'turn_end' })
    const history = useConversationStore.getState().history
    expect(history.at(-1)).toEqual({
      role: 'assistant',
      content: '<action>require_confirm</action><tool>ok</tool>',
    })
    expect(useConversationStore.getState().status).toBe('idle')
  })

  it('tracks confirm payload', () => {
    const payload = { requirements: { topic: 'math', style: 'simple', total_pages: 10, audience: 'kids' } }
    useConversationStore.getState().showConfirm(payload)
    expect(useConversationStore.getState().confirmPayload).toEqual(payload)
    useConversationStore.getState().hideConfirm()
    expect(useConversationStore.getState().confirmPayload).toBeNull()
  })

  it('adds ppt messages', () => {
    useConversationStore.getState().addPPTMessage('ppt done')
    expect(useConversationStore.getState().pptMessages).toHaveLength(1)
    expect(useConversationStore.getState().pptMessages[0].content).toBe('ppt done')
  })
})
