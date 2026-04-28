import type { PPTMessage } from '@/types'

export interface PPTMessagesProps {
  messages: PPTMessage[]
}

/**
 * Displays messages sent from the PPT agent.
 */
export function PPTMessages({ messages }: PPTMessagesProps) {
  if (messages.length === 0) {
    return <div className="ppt-messages empty">No messages from PPT agent yet.</div>
  }

  return (
    <div className="ppt-messages" data-testid="ppt-messages">
      <h4>PPT Agent Messages</h4>
      <ul>
        {messages.map((msg) => (
          <li key={msg.id} className="ppt-message">
            {msg.content}
          </li>
        ))}
      </ul>
    </div>
  )
}
