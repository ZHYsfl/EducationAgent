import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { ConfirmTable } from '../ConfirmTable'

describe('ConfirmTable', () => {
  const requirements = {
    topic: 'Math',
    style: 'Simple',
    total_pages: 10,
    audience: 'Kids',
  }

  it('renders requirements correctly', () => {
    render(<ConfirmTable requirements={requirements} onConfirm={vi.fn()} onDeny={vi.fn()} />)
    expect(screen.getByText('Math')).toBeInTheDocument()
    expect(screen.getByText('Simple')).toBeInTheDocument()
    expect(screen.getByText('10')).toBeInTheDocument()
    expect(screen.getByText('Kids')).toBeInTheDocument()
  })

  it('calls onConfirm when confirm button clicked', () => {
    const onConfirm = vi.fn()
    render(<ConfirmTable requirements={requirements} onConfirm={onConfirm} onDeny={vi.fn()} />)
    fireEvent.click(screen.getByTestId('confirm-btn'))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('calls onDeny when deny button clicked', () => {
    const onDeny = vi.fn()
    render(<ConfirmTable requirements={requirements} onConfirm={vi.fn()} onDeny={onDeny} />)
    fireEvent.click(screen.getByTestId('deny-btn'))
    expect(onDeny).toHaveBeenCalledOnce()
  })
})
