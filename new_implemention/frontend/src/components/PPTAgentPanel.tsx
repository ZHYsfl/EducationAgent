import { useEffect, useRef, useState, useMemo } from 'react'
import { fsList, fsRead, fsDownloadUrl, subscribePPTLog, type FSEntry } from '@/api/client'

interface TreeNode {
  name: string
  path: string
  isDir: boolean
  children?: TreeNode[]
  depth: number
}

function buildTree(entries: FSEntry[]): TreeNode[] {
  const map = new Map<string, TreeNode>()
  const roots: TreeNode[] = []
  const sorted = [...entries].sort((a, b) => a.path.localeCompare(b.path))
  for (const e of sorted) {
    const parts = e.path.split('/')
    const depth = parts.length - 1
    const node: TreeNode = { ...e, depth, children: e.isDir ? [] : undefined }
    map.set(e.path, node)
    const parentPath = parts.slice(0, -1).join('/')
    const parent = map.get(parentPath)
    if (parent?.children) parent.children.push(node)
    else roots.push(node)
  }
  return roots
}

function FileTree({ nodes, selectedFile, onSelect, onDownload }: {
  nodes: TreeNode[]
  selectedFile: string | null
  onSelect: (path: string) => void
  onDownload: (path: string) => void
}) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; path: string } | null>(null)

  const toggle = (path: string) => {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })
  }

  useEffect(() => {
    const close = () => setCtxMenu(null)
    window.addEventListener('click', close)
    return () => window.removeEventListener('click', close)
  }, [])

  return (
    <>
      {ctxMenu && (
        <div className="ctx-menu" style={{ top: ctxMenu.y, left: ctxMenu.x }}>
          <div className="ctx-menu-item" onClick={() => { onDownload(ctxMenu.path); setCtxMenu(null) }}>
            Download
          </div>
        </div>
      )}
      {nodes.map((node) => (
        <div key={node.path}>
          <div
            className={`filetree-entry ${node.isDir ? 'dir' : 'file'} ${selectedFile === node.path ? 'active' : ''}`}
            style={{ paddingLeft: `${8 + node.depth * 14}px` }}
            onClick={() => {
              if (!node.isDir) { onSelect(node.path); return }
              if (node.name === 'node_modules' || node.name === '.git') return
              toggle(node.path)
            }}
            onContextMenu={(e) => {
              if (!node.isDir) {
                e.preventDefault()
                setCtxMenu({ x: e.clientX, y: e.clientY, path: node.path })
              }
            }}
          >
            <span className="filetree-icon">
              {node.isDir ? (expanded.has(node.path) ? '▾' : '▸') : '·'}
            </span>
            <span>{node.name}</span>
          </div>
          {node.isDir && expanded.has(node.path) && node.children && (
            <FileTree nodes={node.children} selectedFile={selectedFile} onSelect={onSelect} onDownload={onDownload} />
          )}
        </div>
      ))}
    </>
  )
}

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

export function PPTAgentPanel() {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [files, setFiles] = useState<FSEntry[]>([])
  const [selectedFile, setSelectedFile] = useState<string | null>(null)
  const [fileContent, setFileContent] = useState<string>('')
  const logEndRef = useRef<HTMLDivElement>(null)
  const logContainerRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const abort = new AbortController()
    subscribePPTLog((line) => {
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

  useEffect(() => {
    const refresh = () => fsList().then(setFiles).catch(() => {})
    refresh()
    const id = setInterval(refresh, 3000)
    return () => clearInterval(id)
  }, [])

  const [isPdf, setIsPdf] = useState(false)

  const openFile = async (path: string) => {
    setSelectedFile(path)
    if (path.endsWith('.pdf')) {
      setIsPdf(true)
      setFileContent('')
      return
    }
    setIsPdf(false)
    const content = await fsRead(path)
    setFileContent(content.length > 500 ? content.slice(0, 500) + '\n... (truncated)' : content)
  }

  const downloadFile = (path: string) => {
    const a = document.createElement('a')
    a.href = fsDownloadUrl(path)
    a.download = path.split('/').pop() ?? path
    a.click()
  }

  const tree = useMemo(() => buildTree(files), [files])

  return (
    <div className="ppt-panel">
      <div className="ppt-panel-left">
        <div className="ppt-filetree">
          <h3>workspace</h3>
          <FileTree nodes={tree} selectedFile={selectedFile} onSelect={openFile} onDownload={downloadFile} />
        </div>
        {selectedFile && (
          <div className="ppt-file-content">
            <div className="file-content-header">{selectedFile.split('/').pop()}</div>
            {isPdf
              ? <iframe src={fsDownloadUrl(selectedFile)} className="pdf-preview" />
              : <pre>{fileContent}</pre>
            }
          </div>
        )}
      </div>
      <div className="ppt-panel-right">
        <h3>PPT Agent</h3>
        <div className="ppt-log" ref={logContainerRef}>
          {logs.map((entry) => <LogLine key={entry.id} entry={entry} />)}
          <div ref={logEndRef} />
        </div>
      </div>
    </div>
  )
}
