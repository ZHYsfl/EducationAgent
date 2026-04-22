import { useState, useEffect, useRef } from 'react'
import { useConversation } from '@/hooks/useConversation'
import { useConversationStore } from '@/store/conversationStore'
import { ConfirmTable } from './ConfirmTable'
import { PPTAgentPanel } from './PPTAgentPanel'

export function Chat() {
  const { start, stop, sendText, status, history, confirmPayload } = useConversation()
  const isPhase2 = useConversationStore((s) => s.isPhase2)
  const [input, setInput] = useState('')
  const historyEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    historyEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [history])

  const handleSend = () => {
    const text = input.trim()
    if (!text || status === 'idle') return
    setInput('')
    sendText(text)
  }

  const voicePanel = (
    <div className="chat-voice-panel">
      <section className="chat-history" data-testid="chat-history">
        {history.length === 0 && (
          <div className="empty-state">Press Start to begin the conversation.</div>
        )}
        {history.map((msg, idx) => (
          <div key={idx} className={`message ${msg.role}`} data-testid={`msg-${msg.role}`}>
            <strong>{msg.role}:</strong>
            <pre>{msg.content}</pre>
          </div>
        ))}
        <div ref={historyEndRef} />
      </section>
      {confirmPayload && (
        <section className="confirm-section">
          <ConfirmTable requirements={confirmPayload.requirements} />
        </section>
      )}
    </div>
  )

  return (
    <div className="chat-container" data-testid="chat-container">
      <header className="chat-header">
        <h1>Education Agent</h1>
        <div className="status-badge" data-testid="status-badge" data-status={status}>
          {status}
        </div>
      </header>

      <div className={`chat-main ${isPhase2 ? 'phase2' : ''}`}>
        {voicePanel}
        {isPhase2 && <PPTAgentPanel />}
      </div>

      <footer className="chat-controls">
        <button onClick={start} disabled={status !== 'idle'} data-testid="start-btn">
          Start Conversation
        </button>
        <button onClick={stop} disabled={status === 'idle'} data-testid="stop-btn">
          Stop
        </button>
        <input
          className="text-input"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && handleSend()}
          placeholder="Type message..."
          disabled={status === 'idle'}
        />
        <button onClick={handleSend} disabled={status === 'idle' || !input.trim()}>
          Send
        </button>
      </footer>
    </div>
  )
}
