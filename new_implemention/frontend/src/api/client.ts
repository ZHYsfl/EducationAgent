import type {
  UniformResponse,
  Requirements,
  UpdateRequirementsData,
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

async function get<T>(path: string): Promise<UniformResponse<T>> {
  const res = await fetch(`${API_BASE}${path}`)
  return res.json() as Promise<UniformResponse<T>>
}

// ---------------------------------------------------------------------------
// Voice Agent APIs
// ---------------------------------------------------------------------------

export async function startConversation(): Promise<UniformResponse<null>> {
  return post<null>('/start_conversation', { from: 'frontend', to: 'voice_agent' })
}

export async function updateRequirements(
  requirements: Partial<Requirements>,
): Promise<UniformResponse<UpdateRequirementsData>> {
  return post<UpdateRequirementsData>('/update_requirements', {
    from: 'frontend',
    to: 'voice_agent',
    requirements,
  })
}

export async function requireConfirm(
  requirements: Requirements,
): Promise<UniformResponse<null>> {
  return post<null>('/require_confirm', {
    from: 'voice_agent',
    to: 'frontend',
    requirements,
  })
}

export async function sendToPPTAgent(data: string): Promise<UniformResponse<null>> {
  return post<null>('/send_to_ppt_agent', {
    from: 'voice_agent',
    to: 'ppt_agent',
    data,
  })
}

export async function fetchFromPPTMessageQueue(): Promise<UniformResponse<string | null>> {
  return get<string | null>('/fetch_from_ppt_message_queue')
}

// ---------------------------------------------------------------------------
// VAD APIs
// ---------------------------------------------------------------------------

export async function vadStart(req: VADStartRequest): Promise<UniformResponse<VADStartData>> {
  return post<VADStartData>('/voice/vad_start', req)
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
