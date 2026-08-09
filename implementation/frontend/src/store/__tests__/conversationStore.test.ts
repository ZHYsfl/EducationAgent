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
      armMessages: [],
      toolBuffer: [],
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
    useConversationStore.getState().handleSSEChunk({ type: 'action', payload: 'send_to_arm_agent:pick up the block' })
    useConversationStore.getState().handleSSEChunk({ type: 'tool', text: 'ok' })
    useConversationStore.getState().handleSSEChunk({ type: 'turn_end' })
    const history = useConversationStore.getState().history
    expect(history.at(-2)).toEqual({
      role: 'assistant',
      content: '<action>send_to_arm_agent:pick up the block</action>',
    })
    expect(history.at(-1)).toEqual({
      role: 'tool',
      content: 'ok',
    })
    expect(useConversationStore.getState().status).toBe('idle')
  })

  it('adds arm messages', () => {
    useConversationStore.getState().addArmMessage('task done')
    expect(useConversationStore.getState().armMessages).toHaveLength(1)
    expect(useConversationStore.getState().armMessages[0].content).toBe('task done')
  })
})
