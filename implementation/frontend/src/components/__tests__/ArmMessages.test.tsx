import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { ArmMessages } from '../ArmMessages'

describe('ArmMessages', () => {
  it('renders empty state', () => {
    render(<ArmMessages messages={[]} />)
    expect(screen.getByText(/No messages from arm agent yet/)).toBeInTheDocument()
  })

  it('renders message list', () => {
    const messages = [
      { id: '1', content: 'Block picked', receivedAt: 0 },
      { id: '2', content: 'Task complete', receivedAt: 1 },
    ]
    render(<ArmMessages messages={messages} />)
    expect(screen.getByText('Block picked')).toBeInTheDocument()
    expect(screen.getByText('Task complete')).toBeInTheDocument()
  })
})
