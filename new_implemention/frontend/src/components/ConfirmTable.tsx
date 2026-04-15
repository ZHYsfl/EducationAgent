import type { Requirements } from '@/types'

export interface ConfirmTableProps {
  requirements: Requirements
  onConfirm: () => void
  onDeny: () => void
}

/**
 * Displays the requirements confirmation table.
 *
 * The user can confirm or deny by speaking; the buttons are also clickable
 * for accessibility and testing.
 */
export function ConfirmTable({ requirements, onConfirm, onDeny }: ConfirmTableProps) {
  return (
    <div className="confirm-table" data-testid="confirm-table">
      <h3>Please confirm the requirements</h3>
      <table>
        <tbody>
          <tr>
            <td>Topic</td>
            <td>{requirements.topic ?? '-'}</td>
          </tr>
          <tr>
            <td>Style</td>
            <td>{requirements.style ?? '-'}</td>
          </tr>
          <tr>
            <td>Total Pages</td>
            <td>{requirements.total_pages ?? '-'}</td>
          </tr>
          <tr>
            <td>Audience</td>
            <td>{requirements.audience ?? '-'}</td>
          </tr>
        </tbody>
      </table>
      <div className="confirm-actions">
        <button onClick={onConfirm} data-testid="confirm-btn">
          Confirm
        </button>
        <button onClick={onDeny} data-testid="deny-btn">
          Deny
        </button>
      </div>
    </div>
  )
}
