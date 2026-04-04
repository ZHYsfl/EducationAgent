package agent

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
)

func (p *Pipeline) drainContextQueue() []ContextMessage {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()

	var msgs []ContextMessage
	if len(p.pendingContexts) > 0 {
		msgs = append(msgs, p.pendingContexts...)
		p.pendingContexts = nil
	}

	for {
		select {
		case msg := <-p.contextQueue:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

func FormatContextForLLM(msgs []ContextMessage) string {
	if len(msgs) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[系统补充信息 - 以下是后台检索到的相关资料，供回答参考]\n")
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("\n--- 操作: %s | 类型: %s ---\n%s\n", m.ActionType, m.MsgType, m.Content))
	}
	return sb.String()
}

func (p *Pipeline) highPriorityListener(ctx context.Context) {
	for {
		select {
		case msg := <-p.highPriorityQueue:
			switch msg.MsgType {
			case "conflict_question", "system_notify":
				if msg.MsgType == "conflict_question" {
					p.session.SendJSON(WSMessage{
						Type:      "conflict_ask",
						TaskID:    msg.Metadata["task_id"],
						PageID:    msg.Metadata["page_id"],
						ContextID: msg.Metadata["context_id"],
						Question:  msg.Content,
					})
				}

				p.session.SetState(StateSpeaking)
				sentenceCh := make(chan string, 1)
				sentenceCh <- msg.Content
				close(sentenceCh)
				p.ttsWorker(ctx, sentenceCh)
				p.session.SetState(StateIdle)

				if ctx.Err() != nil {
					// system_notify 被打断就放弃，不重试
					if msg.MsgType == "system_notify" {
						log.Printf("[high-priority] system_notify interrupted, skipping")
						continue
					}

					// conflict_question 重试机制
					retries := 0
					if r, ok := msg.Metadata["_retries"]; ok {
						fmt.Sscanf(r, "%d", &retries)
					}
					retries++
					if retries > 2 {
						log.Printf("[high-priority] conflict_question interrupted %d times, demoting to context",
							retries)
						p.pendingMu.Lock()
						p.pendingContexts = append(p.pendingContexts, msg)
						p.pendingMu.Unlock()
					} else {
						log.Printf("[high-priority] conflict_question interrupted, will retry (retry=%d)",
							retries)
						if msg.Metadata == nil {
							msg.Metadata = make(map[string]string)
						}
						msg.Metadata["_retries"] = fmt.Sprintf("%d", retries)
						select {
						case p.highPriorityQueue <- msg:
						default:
							p.pendingMu.Lock()
							p.pendingContexts = append(p.pendingContexts, msg)
							p.pendingMu.Unlock()
						}
					}
					continue
				}
				if msg.MsgType == "conflict_question" {
					pageID := msg.Metadata["page_id"]
					baseTS := int64(0)
					if tsStr := msg.Metadata["base_timestamp"]; tsStr != "" {
						if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
							baseTS = ts
						}
					}
					p.session.AddPendingQuestion(msg.Metadata["context_id"], msg.Metadata["task_id"], pageID, baseTS, msg.Content)
				}
			default:
				p.pendingMu.Lock()
				p.pendingContexts = append(p.pendingContexts, msg)
				p.pendingMu.Unlock()
			}
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) enqueueContextMessage(ctx context.Context, msg ContextMessage) {
	switch msg.Priority {
	case "high":
		select {
		case p.highPriorityQueue <- msg:
		case <-ctx.Done():
		}
	default:
		select {
		case p.contextQueue <- msg:
		case <-ctx.Done():
		}
	}
}
