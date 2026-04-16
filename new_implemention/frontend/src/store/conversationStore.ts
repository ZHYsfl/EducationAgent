import { create } from 'zustand'
import type {
  ConversationStatus,
  ConversationMessage,
  ConfirmPayload,
  PPTMessage,
  SSEChunk,
} from '@/types'

export interface ConversationState {
  // -------------------------------------------------------------------------
  // Lifecycle
  // -------------------------------------------------------------------------
  status: ConversationStatus
  setStatus: (status: ConversationStatus) => void

  // -------------------------------------------------------------------------
  // History
  // -------------------------------------------------------------------------
  history: ConversationMessage[]
  appendHistory: (msg: ConversationMessage) => void
  /**
   * Replace the last assistant message (used during streaming to update
   * partial content, or after interruption to truncate).
   */
  replaceLastAssistant: (content: string) => void

  // -------------------------------------------------------------------------
  // Streaming buffer
  // -------------------------------------------------------------------------
  /** Raw assistant text accumulated from the current SSE stream. */
  assistantBuffer: string
  /** Whether the current stream has already emitted an action. */
  hasEnteredActionPhase: boolean
  resetBuffer: () => void
  appendToBuffer: (text: string) => void
  markActionPhase: () => void

  // -------------------------------------------------------------------------
  // Tool result buffer (flushed after the assistant turn ends)
  // -------------------------------------------------------------------------
  toolBuffer: string[]
  resetToolBuffer: () => void
  flushToolBuffer: () => void

  // -------------------------------------------------------------------------
  // TTS playback tracking
  // -------------------------------------------------------------------------
  /** Text that has already been spoken by the TTS engine. */
  spokenText: string
  /** Text that has been buffered but not yet pushed to TTS. */
  ttsPendingText: string
  setSpokenText: (text: string) => void
  setTtsPendingText: (text: string) => void
  consumePendingText: () => string

  // -------------------------------------------------------------------------
  // Interrupt / VAD
  // -------------------------------------------------------------------------
  isInterrupted: boolean
  setInterrupted: (value: boolean) => void

  // -------------------------------------------------------------------------
  // Confirm table
  // -------------------------------------------------------------------------
  confirmPayload: ConfirmPayload | null
  showConfirm: (payload: ConfirmPayload) => void
  hideConfirm: () => void

  // -------------------------------------------------------------------------
  // PPT messages
  // -------------------------------------------------------------------------
  pptMessages: PPTMessage[]
  addPPTMessage: (content: string) => void

  // -------------------------------------------------------------------------
  // SSE chunk handler
  // -------------------------------------------------------------------------
  handleSSEChunk: (chunk: SSEChunk) => void
}

export const useConversationStore = create<ConversationState>((set, get) => ({
  // Lifecycle
  status: 'idle',
  setStatus: (status) => set({ status }),

  // History
  history: [],
  appendHistory: (msg) =>
    set((state) => ({
      history: [...state.history, msg],
    })),
  replaceLastAssistant: (content) =>
    set((state) => {
      const revIdx = [...state.history].reverse().findIndex((m) => m.role === 'assistant')
      if (revIdx < 0) return state
      const idx = state.history.length - 1 - revIdx
      const next = [...state.history]
      next[idx] = { ...next[idx], content }
      return { history: next }
    }),

  // Buffer
  assistantBuffer: '',
  hasEnteredActionPhase: false,
  resetBuffer: () => set({ assistantBuffer: '', hasEnteredActionPhase: false }),
  appendToBuffer: (text) =>
    set((state) => ({
      assistantBuffer: state.assistantBuffer + text,
    })),
  markActionPhase: () => set({ hasEnteredActionPhase: true }),

  // Tool buffer
  toolBuffer: [],
  resetToolBuffer: () => set({ toolBuffer: [] }),
  flushToolBuffer: () =>
    set((state) => {
      const toolMsgs: ConversationMessage[] = state.toolBuffer.map((t) => ({
        role: 'tool',
        content: t,
      }))
      return {
        history: [...state.history, ...toolMsgs],
        toolBuffer: [],
      }
    }),

  // TTS
  spokenText: '',
  ttsPendingText: '',
  setSpokenText: (text) => set({ spokenText: text }),
  setTtsPendingText: (text) => set({ ttsPendingText: text }),
  consumePendingText: () => {
    const text = get().ttsPendingText
    set({ ttsPendingText: '' })
    return text
  },

  // Interrupt
  isInterrupted: false,
  setInterrupted: (value) => set({ isInterrupted: value }),

  // Confirm
  confirmPayload: null,
  showConfirm: (payload) => set({ confirmPayload: payload }),
  hideConfirm: () => set({ confirmPayload: null }),

  // PPT messages
  pptMessages: [],
  addPPTMessage: (content) =>
    set((state) => ({
      pptMessages: [
        ...state.pptMessages,
        { id: crypto.randomUUID(), content, receivedAt: Date.now() },
      ],
    })),

  // SSE handler
  handleSSEChunk: (chunk) => {
    if (chunk.type === 'tts') {
      const text = chunk.text ?? ''
      get().appendToBuffer(text)
      // In a real implementation the pending text would be accumulated
      // and flushed to the TTS engine on punctuation boundaries.
      set((state) => ({
        ttsPendingText: state.ttsPendingText + text,
        status: 'speaking',
      }))
    } else if (chunk.type === 'action') {
      get().markActionPhase()
      get().appendToBuffer(`<action>${chunk.payload ?? ''}</action>`)
      set({ status: 'acting' })
    } else if (chunk.type === 'tool') {
      const toolText = chunk.text ?? ''
      set((state) => ({ toolBuffer: [...state.toolBuffer, toolText] }))
    } else if (chunk.type === 'turn_end') {
      const buffer = get().assistantBuffer
      if (buffer) {
        get().appendHistory({ role: 'assistant', content: buffer })
      }
      get().flushToolBuffer()
      get().resetBuffer()
      set({ status: 'idle' })
    }
  },
}))
