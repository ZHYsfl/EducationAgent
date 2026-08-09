import type { ArmMessage } from '@/types'

export interface ArmMessagesProps {
  messages: ArmMessage[]
}

/**
 * Displays messages sent from the arm agent.
 */
export function ArmMessages({ messages }: ArmMessagesProps) {
  if (messages.length === 0) {
    return <div className="arm-messages empty">No messages from arm agent yet.</div>
  }

  return (
    <div className="arm-messages" data-testid="arm-messages">
      <h4>Arm Agent Messages</h4>
      <ul>
        {messages.map((msg) => (
          <li key={msg.id} className="arm-message">
            {msg.content}
          </li>
        ))}
      </ul>
    </div>
  )
}
