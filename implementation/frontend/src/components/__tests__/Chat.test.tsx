import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { Chat } from '../Chat'
import * as client from '@/api/client'

describe('Chat', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    global.fetch = vi.fn()
  })

  it('renders start button in idle state', () => {
    render(<Chat />)
    expect(screen.getByTestId('start-btn')).toBeInTheDocument()
    expect(screen.getByTestId('stop-btn')).toBeDisabled()
  })

  it('starts conversation and shows listening status', async () => {
    vi.spyOn(client, 'startConversation').mockResolvedValue({
      code: 200,
      message: 'success',
      data: null,
    } as Awaited<ReturnType<typeof client.startConversation>>)

    const getUserMediaMock = vi.fn().mockResolvedValue({
      getTracks: () => [{ stop: vi.fn() }],
    })
    navigator.mediaDevices.getUserMedia = getUserMediaMock

    render(<Chat />)
    fireEvent.click(screen.getByTestId('start-btn'))

    await waitFor(() => expect(screen.getByTestId('status-badge')).toHaveTextContent('listening'))
  })
})
