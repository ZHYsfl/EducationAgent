/**
 * Shared TypeScript types for the Education Agent frontend.
 *
 * These types mirror the backend API contract defined in api.md.
 */

// ---------------------------------------------------------------------------
// API Response Envelope
// ---------------------------------------------------------------------------

export interface UniformResponse<T = unknown> {
  code: number
  message: string
  data: T
}

// ---------------------------------------------------------------------------
// Requirements
// ---------------------------------------------------------------------------

export interface Requirements {
  topic: string | null
  style: string | null
  total_pages: number | null
  audience: string | null
}

export interface UpdateRequirementsData {
  missing_fields: string[] | null
}

// ---------------------------------------------------------------------------
// VAD
// ---------------------------------------------------------------------------

export interface VADStartRequest {
  audio: string
  format: 'pcm'
}

export interface VADStartData {
  interrupt: boolean
}

export interface VADEndRequest {
  audio: string
  format: 'pcm'
  needs_interrupted_prefix?: boolean
  interrupted_assistant_text?: string
}

export interface VADEndIgnoredData {
  ignored: boolean
}

// ---------------------------------------------------------------------------
// SSE Stream
// ---------------------------------------------------------------------------

export type SSEChunkType = 'user_transcript' | 'tts' | 'action' | 'tool' | 'turn_end'

export interface SSEChunk {
  type: SSEChunkType
  text?: string
  payload?: string
}

// ---------------------------------------------------------------------------
// Conversation History
// ---------------------------------------------------------------------------

export type MessageRole = 'user' | 'assistant' | 'system' | 'tool'

export interface ConversationMessage {
  role: MessageRole
  content: string
}

// ---------------------------------------------------------------------------
// Conversation State
// ---------------------------------------------------------------------------

export type ConversationStatus =
  | 'idle'
  | 'listening'
  | 'thinking'
  | 'speaking'
  | 'acting'

export interface ConfirmPayload {
  requirements: Requirements
}

export interface PPTMessage {
  id: string
  content: string
  receivedAt: number
}
