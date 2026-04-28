import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { PPTMessages } from '../PPTMessages'

describe('PPTMessages', () => {
  it('renders empty state', () => {
    render(<PPTMessages messages={[]} />)
    expect(screen.getByText(/No messages from PPT agent yet/)).toBeInTheDocument()
  })

  it('renders message list', () => {
    const messages = [
      { id: '1', content: 'PPT generated', receivedAt: 0 },
      { id: '2', content: 'Export complete', receivedAt: 1 },
    ]
    render(<PPTMessages messages={messages} />)
    expect(screen.getByText('PPT generated')).toBeInTheDocument()
    expect(screen.getByText('Export complete')).toBeInTheDocument()
  })
})
