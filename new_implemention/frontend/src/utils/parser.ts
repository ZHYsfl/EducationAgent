import type { SSEChunk } from '@/types'

/**
 * Extract the spoken text portion from an assistant message that may contain
 * `<action>` and `<tool>` tags.
 */
export function extractSpokenText(assistantContent: string): string {
  // Remove all <action>...</action> and <tool>...</tool> blocks.
  return assistantContent
    .replace(/<action>.*?<\/action>/gs, '')
    .replace(/<tool>.*?<\/tool>/gs, '')
    .trim()
}

/**
 * Build a user message content string for the voice-agent context.
 *
 * @param transcript - the ASR transcript from vad_end
 * @param queueStatus - 'empty' | 'not empty'
 * @param wasInterrupted - whether the previous assistant turn was cut off
 */
export function buildUserContext(
  transcript: string,
  queueStatus: 'empty' | 'not empty',
  wasInterrupted: boolean,
): string {
  const statusLine = `<status>${queueStatus}</status>`
  const userLine = `<user>${transcript}</user>`
  if (wasInterrupted) {
    return `</interrupted>\n${statusLine}\n${userLine}`
  }
  return `${statusLine}\n${userLine}`
}

/**
 * Parse a raw action payload into a structured record.
 * Format: tool_name|k1:v1|k2:v2
 */
export function parseActionPayload(payload: string): {
  name: string
  args: Record<string, string>
} {
  const parts = payload.split('|')
  const name = parts[0] ?? ''
  const args: Record<string, string> = {}
  for (let i = 1; i < parts.length; i++) {
    const [k, ...v] = parts[i].split(':')
    if (k !== undefined) {
      args[k] = v.join(':')
    }
  }
  return { name, args }
}

/**
 * Sentence splitter for TTS buffering.
 * Triggers on Chinese/English sentence-ending punctuation.
 */
export function splitSentences(text: string): string[] {
  // Split on . ! ? 。！？ but keep the punctuation attached to the sentence.
  const sentences: string[] = []
  let current = ''
  for (const char of text) {
    current += char
    if (/[.!?。！？]/.test(char)) {
      sentences.push(current.trim())
      current = ''
    }
  }
  if (current.trim()) {
    sentences.push(current.trim())
  }
  return sentences
}
