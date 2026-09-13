package voiceengine

import "sync"

// ToolCallRecord is one executed tool call and its result.
type ToolCallRecord struct {
	Name     string
	ArgsJSON string
	Result   string
}

// ToolInfo records the tool calls executed during the current turn. The
// producer writes it during the tool phase; compose reads it after <-done.
type ToolInfo struct {
	mu      sync.Mutex
	records []ToolCallRecord
}

func (t *ToolInfo) Add(records ...ToolCallRecord) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = append(t.records, records...)
}

func (t *ToolInfo) Get() []ToolCallRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]ToolCallRecord, len(t.records))
	copy(out, t.records)
	return out
}

func (t *ToolInfo) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.records = nil
}
