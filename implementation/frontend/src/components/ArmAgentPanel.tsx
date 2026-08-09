import { useEffect, useRef, useState } from 'react'
import { subscribeArmLog } from '@/api/client'

interface LogEntry {
  id: number
  type: 'tool' | 'tool_result' | 'tool_error' | 'agent' | 'thinking'
  raw: string
}

function parseLog(raw: string, id: number): LogEntry {
  if (raw.startsWith('[tool_error]')) return { id, type: 'tool_error', raw }
  if (raw.startsWith('[tool_result]')) return { id, type: 'tool_result', raw }
  if (raw.startsWith('[tool]')) return { id, type: 'tool', raw }
  if (raw.startsWith('[agent]')) return { id, type: 'agent', raw }
  if (raw === '[thinking]') return { id, type: 'thinking', raw }
  return { id, type: 'agent', raw }
}

function ThinkingDots() {
  return <span className="thinking-dots"><span>.</span><span>.</span><span>.</span></span>
}

function LogLine({ entry }: { entry: LogEntry }) {
  if (entry.type === 'thinking') {
    return (
      <div className="log-row log-thinking">
        <span className="log-icon">◆</span>
        <span className="log-thinking-text">Thinking<ThinkingDots /></span>
      </div>
    )
  }
  if (entry.type === 'tool') {
    const body = entry.raw.slice('[tool] '.length)
    const spaceIdx = body.indexOf(' ')
    const name = spaceIdx > 0 ? body.slice(0, spaceIdx) : body
    const args = spaceIdx > 0 ? body.slice(spaceIdx + 1) : ''
    return (
      <div className="log-row log-tool">
        <span className="log-icon">⚙</span>
        <span className="log-tool-name">{name}</span>
        {args && <span className="log-tool-args">{args.length > 120 ? args.slice(0, 120) + '…' : args}</span>}
      </div>
    )
  }
  if (entry.type === 'tool_result') {
    const body = entry.raw.slice('[tool_result] '.length)
    return (
      <div className="log-row log-result">
        <span className="log-icon">✓</span>
        <span>{body.length > 150 ? body.slice(0, 150) + '…' : body}</span>
      </div>
    )
  }
  if (entry.type === 'tool_error') {
    const body = entry.raw.slice('[tool_error] '.length)
    return (
      <div className="log-row log-error">
        <span className="log-icon">✗</span>
        <span>{body.length > 150 ? body.slice(0, 150) + '…' : body}</span>
      </div>
    )
  }
  const body = entry.raw.startsWith('[agent] ') ? entry.raw.slice('[agent] '.length) : entry.raw
  return (
    <div className="log-row log-agent">
      <span className="log-icon">◈</span>
      <span>{body.length > 200 ? body.slice(0, 200) + '…' : body}</span>
    </div>
  )
}

let logIdCounter = 0

export function ArmAgentPanel() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const logEndRef = useRef<HTMLDivElement>(null)
  const logContainerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const abort = new AbortController()
    subscribeArmLog((line) => {
      const entry = parseLog(line, ++logIdCounter)
      setLogs((prev) => {
        const next = [...prev.slice(-99), entry]
        if (entry.type !== 'thinking') {
          const lastIdx = next.length - 2
          if (lastIdx >= 0 && next[lastIdx].type === 'thinking') {
            next.splice(lastIdx, 1)
          }
        }
        return next
      })
    }, abort.signal)
    return () => abort.abort()
  }, [])

  return (
    <div className="arm-panel">
      <h3>Arm Agent</h3>
      <div className="arm-log" ref={logContainerRef}>
        {logs.map((entry) => <LogLine key={entry.id} entry={entry} />)}
        <div ref={logEndRef} />
      </div>
    </div>
  )
}
