import { useConversation } from '@/hooks/useConversation'
import { ConfirmTable } from './ConfirmTable'
import { PPTMessages } from './PPTMessages'

/**
 * Main chat interface.
 *
 * Renders the conversation history, status indicator, start/stop controls,
 * the requirements confirmation table, and PPT agent messages.
 */
export function Chat() {
  const { start, stop, status, history, confirmPayload, pptMessages } = useConversation()

  return (
    <div className="chat-container" data-testid="chat-container">
      <header className="chat-header">
        <h1>Education Agent</h1>
        <div className="status-badge" data-testid="status-badge" data-status={status}>
          {status}
        </div>
      </header>

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
      </section>

      {confirmPayload && (
        <section className="confirm-section">
          <ConfirmTable
            requirements={confirmPayload.requirements}
            onConfirm={() => {
              // In a real app this might send a voice command or API call.
              // For now it is a no-op because the voice agent handles confirmation
              // through natural language.
            }}
            onDeny={() => {
              // Same as above: the voice agent handles denial via speech.
            }}
          />
        </section>
      )}

      <section className="ppt-section">
        <PPTMessages messages={pptMessages} />
      </section>

      <footer className="chat-controls">
        <button onClick={start} disabled={status !== 'idle'} data-testid="start-btn">
          Start Conversation
        </button>
        <button onClick={stop} disabled={status === 'idle'} data-testid="stop-btn">
          Stop
        </button>
      </footer>
    </div>
  )
}
