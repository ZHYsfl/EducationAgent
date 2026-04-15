import { useRef, useCallback, useMemo } from 'react'
import { vadEnd } from '@/api/client'
import type { SSEChunk } from '@/types'

export interface UseSSEOptions {
  onChunk: (chunk: SSEChunk) => void
  onError?: (err: Error) => void
}

/**
 * Hook for managing a single `vad_end` SSE connection.
 *
 * Returns a `start` function that initiates the stream and an `abort`
 * function that cancels it early (e.g. on interruption).
 */
export function useSSE(options: UseSSEOptions) {
  const abortControllerRef = useRef<AbortController | null>(null)
  const isRunningRef = useRef(false)
  const onChunkRef = useRef(options.onChunk)
  const onErrorRef = useRef(options.onError)

  onChunkRef.current = options.onChunk
  onErrorRef.current = options.onError

  const start = useCallback(async (audioBase64: string) => {
    if (isRunningRef.current) {
      abortControllerRef.current?.abort('overrun')
    }

    const controller = new AbortController()
    abortControllerRef.current = controller
    isRunningRef.current = true

    try {
      await vadEnd(
        { audio: audioBase64, format: 'pcm' },
        onChunkRef.current,
        controller.signal,
      )
    } catch (err) {
      if (err instanceof Error && err.name !== 'AbortError') {
        onErrorRef.current?.(err)
      }
    } finally {
      isRunningRef.current = false
    }
  }, [])

  const abort = useCallback(() => {
    if (isRunningRef.current) {
      abortControllerRef.current?.abort('user-interrupt')
      isRunningRef.current = false
    }
  }, [])

  const isRunning = useCallback(() => isRunningRef.current, [])

  return useMemo(() => ({ start, abort, isRunning }), [start, abort, isRunning])
}
