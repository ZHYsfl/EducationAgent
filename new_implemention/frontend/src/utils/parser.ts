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
