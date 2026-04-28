import { describe, it, expect } from 'vitest'
import { splitSentences } from '../parser'

describe('splitSentences', () => {
  it('splits on Chinese punctuation', () => {
    expect(splitSentences('你好。世界！')).toEqual(['你好。', '世界！'])
  })

  it('splits on English punctuation', () => {
    expect(splitSentences('Hello world. How are you?')).toEqual([
      'Hello world.',
      'How are you?',
    ])
  })

  it('keeps trailing fragment', () => {
    expect(splitSentences('Hello. This is')).toEqual(['Hello.', 'This is'])
  })
})
