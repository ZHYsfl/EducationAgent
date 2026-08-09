import type {
  UniformResponse,
  VADStartRequest,
  VADStartData,
  VADEndRequest,
  VADEndIgnoredData,
  SSEChunk,
} from '@/types'

const API_BASE = '/api/v1'

async function post<T>(path: string, body: unknown): Promise<UniformResponse<T>> {
  const res = await fetch(`${API_BASE}${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  return res.json() as Promise<UniformResponse<T>>
}

// ---------------------------------------------------------------------------
// Voice Agent APIs
// ---------------------------------------------------------------------------

export async function startConversation(): Promise<UniformResponse<null>> {
  return post<null>('/start_conversation', { from: 'frontend', to: 'voice_agent' })
}

// ---------------------------------------------------------------------------
// Arm Agent APIs
// ---------------------------------------------------------------------------

export async function sendToArmAgent(content: string): Promise<UniformResponse<string>> {
  return post<string>('/send_to_arm_agent', {
    from: 'frontend',
    to: 'arm_agent',
    content,
  })
}

export async function getMessageFromArmAgent(): Promise<UniformResponse<string | null>> {
  return post<string | null>('/get_message_from_arm_agent', {})
}

// ---------------------------------------------------------------------------
// VAD APIs
// ---------------------------------------------------------------------------

export async function vadStart(req: VADStartRequest, signal?: AbortSignal): Promise<UniformResponse<VADStartData>> {
  const res = await fetch(`${API_BASE}/voice/vad_start`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
    signal,
  })
  return res.json() as Promise<UniformResponse<VADStartData>>
}

export async function vadEnd(
  req: VADEndRequest,
  onChunk: (chunk: SSEChunk) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/voice/vad_end`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
    signal,
  })

  if (!res.ok) {
    throw new Error(`vad_end request failed: ${res.status}`)
  }

  const contentType = res.headers.get('Content-Type') || ''
  const isSSE = contentType.includes('text/event-stream')

  // When interrupt == false the backend returns a plain JSON response.
  if (!isSSE) {
    const json = (await res.json()) as UniformResponse<unknown>
    if (json.data && isIgnoredResponse(json.data)) {
      // Synthesize a single turn_end so the caller can treat it uniformly.
      onChunk({ type: 'turn_end' })
    }
    return
  }

  if (!res.body) {
    throw new Error('vad_end SSE response has no body')
  }

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  try {
    while (true) {
      if (signal?.aborted) {
        reader.cancel()
        throw signal.reason
      }

      const { done, value } = await reader.read()
      if (done) break

      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''

      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed.startsWith('data: ')) continue
        const payload = trimmed.slice(6).trim()
        if (payload === '[DONE]') return
        if (!payload) continue
        try {
          const chunk = JSON.parse(payload) as SSEChunk
          onChunk(chunk)
        } catch {
          // ignore malformed JSON lines
        }
      }
    }
  } finally {
    reader.cancel().catch(() => {})
  }
}

export function isIgnoredResponse(data: unknown): data is VADEndIgnoredData {
  return typeof data === 'object' && data !== null && 'ignored' in data
}

// ---------------------------------------------------------------------------
// Text input
// ---------------------------------------------------------------------------

export async function textInput(
  text: string,
  onChunk: (chunk: SSEChunk) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch(`${API_BASE}/voice/text_input`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
    signal,
  })
  if (!res.ok || !res.body) throw new Error(`text_input failed: ${res.status}`)

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      if (signal?.aborted) { reader.cancel(); throw signal.reason }
      const { done, value } = await reader.read()
      if (done) break
      buffer += decoder.decode(value, { stream: true })
      const lines = buffer.split('\n')
      buffer = lines.pop() ?? ''
      for (const line of lines) {
        const trimmed = line.trim()
        if (!trimmed.startsWith('data: ')) continue
        const payload = trimmed.slice(6).trim()
        if (payload === '[DONE]') return
        if (!payload) continue
        try { onChunk(JSON.parse(payload) as SSEChunk) } catch {}
      }
    }
  } finally {
    reader.cancel().catch(() => {})
  }
}

// ---------------------------------------------------------------------------
// Arm agent activity log stream
// ---------------------------------------------------------------------------

export function subscribeArmLog(onLine: (line: string) => void, signal: AbortSignal): void {
  const es = new EventSource(`${API_BASE}/arm/log-stream`)
  signal.addEventListener('abort', () => es.close())
  es.onmessage = (e) => {
    try {
      onLine(JSON.parse(e.data) as string)
    } catch {
      onLine(e.data)
    }
  }
}
