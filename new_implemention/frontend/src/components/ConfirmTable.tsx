import type { Requirements } from '@/types'

export interface ConfirmTableProps {
  requirements: Requirements
}

export function ConfirmTable({ requirements }: ConfirmTableProps) {
  return (
    <div className="confirm-table" data-testid="confirm-table">
      <h3>Please confirm the requirements</h3>
      <table>
        <tbody>
          <tr><td>Topic</td><td>{requirements.topic ?? '-'}</td></tr>
          <tr><td>Style</td><td>{requirements.style ?? '-'}</td></tr>
          <tr><td>Total Pages</td><td>{requirements.total_pages ?? '-'}</td></tr>
          <tr><td>Audience</td><td>{requirements.audience ?? '-'}</td></tr>
        </tbody>
      </table>
    </div>
  )
}
