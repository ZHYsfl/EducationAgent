import { useRef, useCallback, useEffect } from 'react'
import { useConversationStore } from '@/store/conversationStore'
import { AudioRecorder } from '@/audio/recorder'
import { VADetector } from '@/audio/vad'
import { TTSEngine } from '@/audio/tts'
import { useSSE } from './useSSE'
import {
  vadStart,
  startConversation as apiStartConversation,
  releaseSlidevPreview,
} from '@/api/client'
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
  const asrStartTimeRef = useRef<number>(0)
  const fastCheckPromiseRef = useRef<Promise<{ interrupt: boolean }> | null>(null)
  const fastCheckAbortRef = useRef<AbortController | null>(null)
  const fastCheckSentRef = useRef<boolean>(false)
  const interruptPendingRef = useRef<boolean>(false)
  const fastCheckTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const ttsWasActiveRef = useRef<boolean>(false)

  // Accumulate raw SSE text so we can rebuild the exact assistant message.
  const streamHistoryRef = useRef<string>('')
  const pendingToolsRef = useRef<string[]>([])
  // In the two-round architecture (fetch -> second inference), this marks the
  // offset in streamHistoryRef right after the last action tag, separating
  // round-1 assistant from round-2 assistant text.
  const postActionOffsetRef = useRef<number>(0)
  const startCancelledRef = useRef<boolean>(false)
  const activeRef = useRef<boolean>(false)

  const spokenTextRef = useRef<string>('')
  const ttsPendingRef = useRef<string>('')  // -------------------------------------------------------------------------
  // TTS helpers
  // -------------------------------------------------------------------------
  const flushTTS = useCallback(() => {
    const pending = ttsPendingRef.current
    if (!pending) return
    ttsRef.current?.enqueue(pending)
    ttsPendingRef.current = ''
    store.setTtsPendingText('')
  }, [store])

  const clearTTSAndPlayback = useCallback(() => {
    ttsRef.current?.clear()
    ttsPendingRef.current = ''
    store.setTtsPendingText('')
  }, [store])

  // -------------------------------------------------------------------------
  // SSE chunk handler
  // -------------------------------------------------------------------------
  const handleChunk = useCallback(
    (chunk: SSEChunk) => {
      if (!activeRef.current) return
      if (chunk.type === 'user_transcript') {
        store.appendHistory({ role: 'user', content: chunk.text ?? '' })
      } else if (chunk.type === 'tts') {
        const text = chunk.text ?? ''
        streamHistoryRef.current += text
        store.appendToBuffer(text)
        ttsPendingRef.current += text
        store.setTtsPendingText(ttsPendingRef.current)
        const sentences = splitSentences(ttsPendingRef.current)
        if (sentences.length > 1) {
          for (let i = 0; i < sentences.length - 1; i++) {
            ttsRef.current?.enqueue(sentences[i])
          }
          ttsPendingRef.current = sentences[sentences.length - 1] ?? ''
          store.setTtsPendingText(ttsPendingRef.current)
        }
      } else if (chunk.type === 'action') {
        store.markActionPhase()
        const actionTag = `<action>${chunk.payload ?? ''}</action>`
        streamHistoryRef.current += actionTag
        store.appendToBuffer(actionTag)
        store.setStatus('acting')
      } else if (chunk.type === 'tool') {
        const toolText = chunk.text ?? ''
        store.hideConfirm()
        if (toolText.startsWith('require_confirm:')) {
          try {
            const req = JSON.parse(toolText.slice('require_confirm:'.length))
            store.showConfirm({ requirements: req })
          } catch {}
        }
        if (toolText.includes('data is sent to the ppt agent successfully')) {
          store.setPhase2(true)
        }
        pendingToolsRef.current.push(toolText)
        // Mark the boundary after the last action tag for round-2 detection.
        postActionOffsetRef.current = streamHistoryRef.current.length
      } else if (chunk.type === 'turn_end') {
        flushTTS()
        const content = streamHistoryRef.current
        if (postActionOffsetRef.current > 0) {
          const pre = content.slice(0, postActionOffsetRef.current)
          const post = content.slice(postActionOffsetRef.current)
          if (pre) {
            store.appendHistory({ role: 'assistant', content: pre })
          }
          for (const toolText of pendingToolsRef.current) {
            store.appendHistory({ role: 'tool', content: toolText })
          }
          if (post) {
            store.appendHistory({ role: 'assistant', content: post })
          }
        } else if (content) {
          store.appendHistory({ role: 'assistant', content })
        }
        pendingToolsRef.current = []
        streamHistoryRef.current = ''
        postActionOffsetRef.current = 0
        store.resetBuffer()
        spokenTextRef.current = ''
        store.setSpokenText('')
      }
    },
    [store, flushTTS],
  )

  const sse = useSSE({
    onChunk: handleChunk,
    onError: (err) => {
      if (!activeRef.current) return
      console.error('SSE error:', err)
      store.setStatus('idle')
    },
  })

  // -------------------------------------------------------------------------
  // Interrupt handling
  // -------------------------------------------------------------------------
  const performInterrupt = useCallback(async () => {
    // 1. Record whether TTS is still playing so we can decide later whether
    //    to prepend </interrupted> even if ttsPendingText has been flushed.
    ttsWasActiveRef.current = ttsRef.current?.isActive() ?? false
    // 2. Stop playback immediately.
    clearTTSAndPlayback()
    // 3. Abort the backend stream.
    sse.abort()
    interruptPendingRef.current = true
  }, [clearTTSAndPlayback, sse])

  const finalizeInterruptedTurn = useCallback(() => {
    // Called when vad_end arrives after an interrupt.
    const spokenText = store.spokenText
    const streamContent = streamHistoryRef.current
    const postActionOffset = postActionOffsetRef.current
    let interruptedAssistantText = ''

    if (postActionOffset > 0) {
      // There is at least one complete action tag. Split into round 1 and round 2.
      const pre = streamContent.slice(0, postActionOffset)

      if (pre) {
        store.appendHistory({ role: 'assistant', content: pre })
        interruptedAssistantText = pre
      }
      for (const toolText of pendingToolsRef.current) {
        store.appendHistory({ role: 'tool', content: toolText })
      }

      // Round 2 (if any) only keeps what was actually spoken.
      // spokenText contains raw audio text without action tags.
      // We approximate the round-2 portion by stripping the round-1 text.
      const preTextOnly = pre.replace(/<action>.*?\n?<\/action>/gs, '')
      const round2Spoken = spokenText.startsWith(preTextOnly)
        ? spokenText.slice(preTextOnly.length).trimStart()
        : ''
      if (round2Spoken) {
        store.appendHistory({ role: 'assistant', content: round2Spoken })
        interruptedAssistantText = round2Spoken
      }
    } else {
      // No action tags: simple truncation to what was actually spoken.
      if (spokenText) {
        store.appendHistory({ role: 'assistant', content: spokenText + ' ✂' })
        interruptedAssistantText = spokenText
      }
    }

    pendingToolsRef.current = []
    streamHistoryRef.current = ''
    postActionOffsetRef.current = 0
    store.resetBuffer()
    spokenTextRef.current = ''
    store.setSpokenText('')
    interruptPendingRef.current = false
    return interruptedAssistantText
  }, [store])

  // -------------------------------------------------------------------------
  // VAD callbacks
  // -------------------------------------------------------------------------
  const sendFastCheck = useCallback(async () => {
    if (fastCheckSentRef.current) {
      // Already triggered by timeout; wait for the in-flight promise if any.
      await (fastCheckPromiseRef.current ?? Promise.resolve())
      return
    }
    fastCheckSentRef.current = true

    const recorder = recorderRef.current
    if (!recorder) return

    const now = recorder.getElapsedMs()
    const start = speechStartTimeRef.current
    const end = Math.min(now, start + VAD_FAST_CHECK_MS)

    const segment = recorder.extractSegment(start, end)
    if (!segment) return

    const abortController = new AbortController()
    fastCheckAbortRef.current = abortController

    const promise = vadStart({ audio: segment.base64, format: 'pcm' }, abortController.signal)
      .then((res) => res.data ?? { interrupt: false })
      .catch(() => ({ interrupt: false }))

    fastCheckPromiseRef.current = promise

    const result = await promise
    if (result.interrupt) {
      await performInterrupt()
    }
  }, [performInterrupt])

  const handleSpeechStart = useCallback(() => {
    if (!activeRef.current || !recorderRef.current) return
    store.setStatus('speaking')
    const elapsed = recorderRef.current?.getElapsedMs() ?? 0
    speechStartTimeRef.current = elapsed
    asrStartTimeRef.current = Math.max(0, elapsed - 2000)
    fastCheckSentRef.current = false
    fastCheckPromiseRef.current = null
    interruptPendingRef.current = false
    pendingToolsRef.current = []
    ttsWasActiveRef.current = false

    if (fastCheckTimeoutRef.current) {
      clearTimeout(fastCheckTimeoutRef.current)
    }
    fastCheckTimeoutRef.current = setTimeout(() => {
      sendFastCheck()
    }, VAD_FAST_CHECK_MS)
  }, [sendFastCheck, store])

  const handleSpeechEnd = useCallback(async () => {
    // User clicked Stop: ignore any delayed VAD callbacks.
    if (!activeRef.current || !vadRef.current || !recorderRef.current) return

    store.setStatus('thinking')

    try {
      await sendFastCheck()

      const recorder = recorderRef.current
      if (!recorder) return

      const now = recorder.getElapsedMs()
      const start = asrStartTimeRef.current
      const segment = recorder.extractSegment(start, now)
      if (!segment) {
        // No audio captured yet; treat as a no-op turn.
        if (activeRef.current) store.setStatus('listening')
        return
      }

      const fastResult = await (fastCheckPromiseRef.current ?? Promise.resolve({ interrupt: false }))

      // User may have clicked Stop while we were awaiting; abort if cleaned up.
      if (!recorderRef.current) return

      const needsPrefix = fastResult.interrupt && (store.ttsPendingText !== '' || ttsWasActiveRef.current)

      let interruptedAssistantText = ''
      if (fastResult.interrupt) {
        // Interrupt path: finalize the truncated turn, then start the new SSE stream.
        interruptedAssistantText = finalizeInterruptedTurn()
      }

      const req: import('@/types').VADEndRequest = {
        audio: segment.base64,
        format: 'pcm',
        needs_interrupted_prefix: needsPrefix,
        interrupted_assistant_text: interruptedAssistantText,
      }

      if (!fastResult.interrupt) {
        await sse.start(req)
        recorderRef.current?.resetBuffer()
        if (activeRef.current) store.setStatus('listening')
        ttsWasActiveRef.current = false
        return
      }

      await sse.start(req)
      recorderRef.current?.resetBuffer()
      if (activeRef.current) store.setStatus('listening')
      ttsWasActiveRef.current = false
    } catch (err) {
      console.error('handleSpeechEnd error:', err)
      recorderRef.current?.resetBuffer()
      if (activeRef.current) store.setStatus('listening')
    }
  }, [sendFastCheck, sse, store, finalizeInterruptedTurn])

  // -------------------------------------------------------------------------
  // Conversation lifecycle
  // -------------------------------------------------------------------------
  const start = useCallback(async () => {
    startCancelledRef.current = false
    activeRef.current = true
    store.clearHistory()
    store.setPhase2(false)

    try {
    const res = await apiStartConversation()
    if (res.code !== 200) {
      activeRef.current = false
      store.setStatus('idle')
      throw new Error(res.message || 'failed to start conversation')
    }

    store.setStatus('listening')

    const recorder = new AudioRecorder()
    await recorder.start()
    if (startCancelledRef.current) {
      recorder.stop()
      return
    }
    recorderRef.current = recorder

    const tts = new TTSEngine()
    tts.setOnSentenceEnd((text) => {
      spokenTextRef.current += text
      store.setSpokenText(spokenTextRef.current)
    })
    ttsRef.current = tts

    const vad = new VADetector(
      { thresholdDb: -35, minSpeechDurationMs: 300, minSilenceDurationMs: 300 },
      {
        onSpeechStart: handleSpeechStart,
        onSpeechEnd: handleSpeechEnd,
      },
    )
    await vad.start()
    if (startCancelledRef.current) {
      vad.stop()
      recorder.stop()
      recorderRef.current = null
      return
    }
    vadRef.current = vad
    } catch (err) {
      activeRef.current = false
      store.setStatus('idle')
      throw err
    }
  }, [handleSpeechEnd, handleSpeechStart, store])

  const stop = useCallback(() => {
    activeRef.current = false
    startCancelledRef.current = true
    if (fastCheckTimeoutRef.current) {
      clearTimeout(fastCheckTimeoutRef.current)
      fastCheckTimeoutRef.current = null
    }
    fastCheckAbortRef.current?.abort()
    fastCheckAbortRef.current = null
    sse.abort()
    ttsRef.current?.clear()
    vadRef.current?.stop()
    recorderRef.current?.stop()
    vadRef.current = null
    recorderRef.current = null
    ttsRef.current = null
    pendingToolsRef.current = []
    streamHistoryRef.current = ''
    postActionOffsetRef.current = 0
    fastCheckSentRef.current = false
    fastCheckPromiseRef.current = null
    interruptPendingRef.current = false
    ttsWasActiveRef.current = false
    ttsPendingRef.current = ''
    store.setStatus('idle')
    void releaseSlidevPreview().catch(() => {})
  }, [sse, store])

  const sendText = useCallback(async (text: string) => {
    if (!activeRef.current) return
    store.setStatus('thinking')

    const ttsActive = ttsRef.current?.isActive() ?? false
    const needsPrefix = ttsActive || store.ttsPendingText !== ''
    let interruptedAssistantText = ''

    if (ttsActive || interruptPendingRef.current) {
      // TTS was playing — do a proper interrupt with history truncation.
      ttsWasActiveRef.current = ttsActive
      clearTTSAndPlayback()
      sse.abort()
      interruptedAssistantText = finalizeInterruptedTurn()
    } else {
      // No TTS playing — just abort any in-flight SSE and discard its buffer.
      sse.abort()
      streamHistoryRef.current = ''
      postActionOffsetRef.current = 0
      pendingToolsRef.current = []
      store.resetBuffer()
      spokenTextRef.current = ''
      store.setSpokenText('')
    }

    streamHistoryRef.current = ''
    postActionOffsetRef.current = 0
    pendingToolsRef.current = []

    const req: import('@/types').VADEndRequest = {
      audio: '',
      format: 'pcm',
      text,
      needs_interrupted_prefix: needsPrefix,
      interrupted_assistant_text: interruptedAssistantText,
    }
    try {
      await sse.start(req)
    } catch (err) {
      console.error('sendText error:', err)
    }
    if (activeRef.current) store.setStatus('listening')
  }, [sse, store, clearTTSAndPlayback, finalizeInterruptedTurn])

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
    sendText,
    status: store.status,
    history: store.history,
    confirmPayload: store.confirmPayload,
    pptMessages: store.pptMessages,
  }
}
