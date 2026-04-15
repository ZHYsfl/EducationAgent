import { describe, it, expect } from 'vitest'
import {
  extractSpokenText,
  buildUserContext,
  parseActionPayload,
  splitSentences,
} from '../parser'

describe('extractSpokenText', () => {
  it('returns plain text unchanged', () => {
    expect(extractSpokenText('hello world')).toBe('hello world')
  })

  it('strips action and tool tags', () => {
    const input = 'hello <action>update_requirements|topic:math</action><tool>ok</tool> world'
    expect(extractSpokenText(input)).toBe('hello  world')
  })
})

describe('buildUserContext', () => {
  it('includes interrupted tag when interrupted', () => {
    const text = buildUserContext('你好', 'not empty', true)
    expect(text).toBe('</interrupted>\n<status>not empty</status>\n<user>你好</user>')
  })

  it('omits interrupted tag when not interrupted', () => {
    const text = buildUserContext('hello', 'empty', false)
    expect(text).toBe('<status>empty</status>\n<user>hello</user>')
  })
})

describe('parseActionPayload', () => {
  it('parses action without args', () => {
    expect(parseActionPayload('require_confirm')).toEqual({
      name: 'require_confirm',
      args: {},
    })
  })

  it('parses action with args', () => {
    expect(parseActionPayload('update_requirements|topic:math|style:simple')).toEqual({
      name: 'update_requirements',
      args: { topic: 'math', style: 'simple' },
    })
  })

  it('preserves colons inside values', () => {
    expect(parseActionPayload('send_to_ppt_agent|data:a:b:c')).toEqual({
      name: 'send_to_ppt_agent',
      args: { data: 'a:b:c' },
    })
  })
})

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
