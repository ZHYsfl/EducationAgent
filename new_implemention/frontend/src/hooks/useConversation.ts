import { useRef, useCallback, useEffect } from 'react'
import { useConversationStore } from '@/store/conversationStore'
import { AudioRecorder } from '@/audio/recorder'
import { VADetector } from '@/audio/vad'
import { TTSEngine } from '@/audio/tts'
import { useSSE } from './useSSE'
import { vadStart, startConversation as apiStartConversation } from '@/api/client'
import { splitSentences } from '@/utils/parser'
import type { SSEChunk } from '@/types'

const VAD_FAST_CHECK_MS = 1500

/**
 * Orchestrates the full voice conversation lifecycle:
 * - microphone capture with VAD
 * - fast-interrupt checks (`vad_start`)
 * - SSE streaming (`vad_end`)
 * - TTS playback queue
 * - conversation history construction (including interruption recovery)
 */
export function useConversation() {
  const store = useConversationStore()

  // -------------------------------------------------------------------------
  // Refs for mutable hardware / stream state
  // -------------------------------------------------------------------------
  const recorderRef = useRef<AudioRecorder | null>(null)
  const vadRef = useRef<VADetector | null>(null)
  const ttsRef = useRef<TTSEngine | null>(null)

  const speechStartTimeRef = useRef<number>(0)
  const fastCheckPromiseRef = useRef<Promise<{ interrupt: boolean }> | null>(null)
  const fastCheckSentRef = useRef<boolean>(false)
  const interruptPendingRef = useRef<boolean>(false)
  const actionStartedRef = useRef<boolean>(false)
  const fastCheckTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Accumulate raw SSE text so we can rebuild the exact assistant message.
  const streamHistoryRef = useRef<string>('')
  const pendingToolsRef = useRef<string[]>([])

  // -------------------------------------------------------------------------
  // TTS helpers
  // -------------------------------------------------------------------------
  const flushTTS = useCallback(() => {
    const pending = store.ttsPendingText
    if (!pending) return
    ttsRef.current?.enqueue(pending)
    store.setTtsPendingText('')
  }, [store])

  const clearTTSAndPlayback = useCallback(() => {
    ttsRef.current?.clear()
    store.setTtsPendingText('')
  }, [store])

  // -------------------------------------------------------------------------
  // SSE chunk handler
  // -------------------------------------------------------------------------
  const handleChunk = useCallback(
    (chunk: SSEChunk) => {
      if (chunk.type === 'tts') {
        const text = chunk.text ?? ''
        streamHistoryRef.current += text
        store.appendToBuffer(text)
        const nextPending = store.ttsPendingText + text
        store.setTtsPendingText(nextPending)
        // Sentence-level flush.
        const sentences = splitSentences(nextPending)
        if (sentences.length > 1) {
          for (let i = 0; i < sentences.length - 1; i++) {
            ttsRef.current?.enqueue(sentences[i])
          }
          store.setTtsPendingText(sentences[sentences.length - 1] ?? '')
        }
      } else if (chunk.type === 'action') {
        actionStartedRef.current = true
        store.markActionPhase()
        const actionTag = `<action>${chunk.payload ?? ''}</action>`
        streamHistoryRef.current += actionTag
        store.appendToBuffer(actionTag)
        store.setStatus('acting')
      } else if (chunk.type === 'tool') {
        const toolText = chunk.text ?? ''
        pendingToolsRef.current.push(toolText)
      } else if (chunk.type === 'turn_end') {
        flushTTS()
        const content = streamHistoryRef.current
        if (content) {
          store.appendHistory({ role: 'assistant', content })
        }
        for (const toolText of pendingToolsRef.current) {
          store.appendHistory({ role: 'tool', content: toolText })
        }
        pendingToolsRef.current = []
        streamHistoryRef.current = ''
        store.resetBuffer()
        actionStartedRef.current = false
        interruptPendingRef.current = false
        store.setStatus('idle')
      }
    },
    [store, flushTTS],
  )

  const sse = useSSE({
    onChunk: handleChunk,
    onError: (err) => {
      console.error('SSE error:', err)
      store.setStatus('idle')
    },
  })

  // -------------------------------------------------------------------------
  // Interrupt handling
  // -------------------------------------------------------------------------
  const performInterrupt = useCallback(async () => {
    // 1. Stop playback immediately.
    clearTTSAndPlayback()
    // 2. Abort the backend stream.
    sse.abort()
    interruptPendingRef.current = true
  }, [clearTTSAndPlayback, sse])

  const finalizeInterruptedTurn = useCallback(() => {
    // Called when vad_end arrives after an interrupt.
    // Build the truncated assistant message from what was already spoken
    // plus any complete action sequence.
    const spokenText = store.spokenText
    const streamContent = streamHistoryRef.current
    const hadEnteredAction = actionStartedRef.current

    let assistantContent = spokenText
    if (hadEnteredAction && streamContent) {
      // Always preserve only the actually spoken text plus complete action tags.
      // Do NOT include unplayed post-action TTS that arrived after the interrupt.
      const actionTags = streamContent.match(/<action>.*?<\/action>/gs) || []
      const tail = actionTags.join('')
      assistantContent = (spokenText + ' ' + tail).trim()
    }

    if (assistantContent) {
      const revIdx = [...store.history].reverse().findIndex((m) => m.role === 'assistant')
      const lastAssistantIdx = revIdx >= 0 ? store.history.length - 1 - revIdx : -1
      if (lastAssistantIdx >= 0) {
        // Check whether the last assistant message in history corresponds
        // to the current streaming turn. We treat it as the same turn if
        // its content is a prefix of the stream content.
        const last = store.history[lastAssistantIdx]
        if (last?.role === 'assistant' && streamContent.startsWith(last.content)) {
          store.replaceLastAssistant(assistantContent)
        } else {
          store.appendHistory({ role: 'assistant', content: assistantContent })
        }
      } else {
        store.appendHistory({ role: 'assistant', content: assistantContent })
      }
    }

    for (const toolText of pendingToolsRef.current) {
      store.appendHistory({ role: 'tool', content: toolText })
    }
    pendingToolsRef.current = []

    streamHistoryRef.current = ''
    store.resetBuffer()
    store.setSpokenText('')
    actionStartedRef.current = false
    interruptPendingRef.current = false
  }, [store])

  // -------------------------------------------------------------------------
  // VAD callbacks
  // -------------------------------------------------------------------------
  const sendFastCheck = useCallback(async () => {
    if (fastCheckSentRef.current) return
    fastCheckSentRef.current = true

    const recorder = recorderRef.current
    if (!recorder) return

    const now = recorder.getElapsedMs()
    const start = speechStartTimeRef.current
    const end = Math.min(now, start + VAD_FAST_CHECK_MS)

    const segment = recorder.extractSegment(start, end)
    if (!segment) return

    const promise = vadStart({ audio: segment.base64, format: 'pcm' }).then((res) => {
      return res.data ?? { interrupt: false }
    })

    fastCheckPromiseRef.current = promise

    const result = await promise
    if (result.interrupt) {
      await performInterrupt()
    }
  }, [performInterrupt])

  const handleSpeechStart = useCallback(() => {
    speechStartTimeRef.current = recorderRef.current?.getElapsedMs() ?? 0
    fastCheckSentRef.current = false
    fastCheckPromiseRef.current = null
    interruptPendingRef.current = false
    actionStartedRef.current = false
    pendingToolsRef.current = []

    if (fastCheckTimeoutRef.current) {
      clearTimeout(fastCheckTimeoutRef.current)
    }
    fastCheckTimeoutRef.current = setTimeout(() => {
      sendFastCheck()
    }, VAD_FAST_CHECK_MS)
  }, [sendFastCheck])

  const handleSpeechEnd = useCallback(async () => {
    store.setStatus('thinking')

    if (!fastCheckSentRef.current) {
      await sendFastCheck()
    }

    const recorder = recorderRef.current
    if (!recorder) return

    const fullSegment = recorder.getFullSegment()
    if (!fullSegment) return

    const fastResult = await (fastCheckPromiseRef.current ?? Promise.resolve({ interrupt: false }))

    if (!fastResult.interrupt) {
      // Backend returns a plain JSON response; vadEnd will synthesise turn_end.
      await sse.start(fullSegment.base64)
      store.setStatus('idle')
      return
    }

    // Interrupt path: finalize the truncated turn, then start the new SSE stream.
    finalizeInterruptedTurn()
    await sse.start(fullSegment.base64)
  }, [sendFastCheck, sse, store, finalizeInterruptedTurn])

  // -------------------------------------------------------------------------
  // Conversation lifecycle
  // -------------------------------------------------------------------------
  const start = useCallback(async () => {
    const res = await apiStartConversation()
    if (res.code !== 200) {
      throw new Error(res.message || 'failed to start conversation')
    }

    store.setStatus('listening')

    const recorder = new AudioRecorder()
    await recorder.start()
    recorderRef.current = recorder

    const tts = new TTSEngine()
    tts.setOnSentenceEnd((text) => {
      store.setSpokenText(store.spokenText + text)
    })
    ttsRef.current = tts

    const vad = new VADetector(
      { thresholdDb: -40, minSpeechDurationMs: 200, minSilenceDurationMs: 500 },
      {
        onSpeechStart: handleSpeechStart,
        onSpeechEnd: handleSpeechEnd,
      },
    )
    await vad.start()
    vadRef.current = vad
  }, [handleSpeechEnd, handleSpeechStart, store])

  const stop = useCallback(() => {
    if (fastCheckTimeoutRef.current) {
      clearTimeout(fastCheckTimeoutRef.current)
      fastCheckTimeoutRef.current = null
    }
    sse.abort()
    ttsRef.current?.clear()
    vadRef.current?.stop()
    recorderRef.current?.stop()
    vadRef.current = null
    recorderRef.current = null
    ttsRef.current = null
    pendingToolsRef.current = []
    store.setStatus('idle')
  }, [sse, store])

  const stopRef = useRef(stop)
  stopRef.current = stop

  useEffect(() => {
    return () => {
      stopRef.current()
    }
  }, [])

  return {
    start,
    stop,
    status: store.status,
    history: store.history,
    confirmPayload: store.confirmPayload,
    pptMessages: store.pptMessages,
  }
}
