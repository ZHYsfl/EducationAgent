package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
)

func (p *Pipeline) drainContextQueue() []ContextMessage {
	var msgs []ContextMessage

	p.pendingMu.Lock()
	if len(p.pendingContexts) > 0 {
		msgs = append(msgs, p.pendingContexts...)
		p.pendingContexts = p.pendingContexts[:0]
	}
	p.pendingMu.Unlock()

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
		sb.WriteString(fmt.Sprintf("\n--- 来源: %s | 类型: %s ---\n%s\n", m.Source, m.MsgType, m.Content))
	}
	return sb.String()
}

func (p *Pipeline) highPriorityListener(ctx context.Context) {
	for {
		select {
		case msg := <-p.highPriorityQueue:
			switch msg.MsgType {
			case "conflict_question":
				p.session.SendJSON(WSMessage{
					Type:      "conflict_ask",
					TaskID:    msg.Metadata["task_id"],
					PageID:    msg.Metadata["page_id"],
					ContextID: msg.Metadata["context_id"],
					Question:  msg.Content,
				})

				p.session.SetState(StateSpeaking)
				sentenceCh := make(chan string, 1)
				sentenceCh <- msg.Content
				close(sentenceCh)
				p.ttsWorker(ctx, sentenceCh)
				p.session.SetState(StateIdle)

				if ctx.Err() != nil {
					retries := 0
					if r, ok := msg.Metadata["_retries"]; ok {
						fmt.Sscanf(r, "%d", &retries)
					}
					retries++
					if retries > 2 {
						log.Printf("[high-priority] conflict_question interrupted %d times, demoting to context context_id=%s",
							retries, msg.Metadata["context_id"])
						p.pendingMu.Lock()
						p.pendingContexts = append(p.pendingContexts, msg)
						p.pendingMu.Unlock()
					} else {
						log.Printf("[high-priority] conflict_question interrupted, will re-ask (retry=%d) context_id=%s",
							retries, msg.Metadata["context_id"])
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
					return
				}
				p.session.AddPendingQuestion(msg.Metadata["context_id"], msg.Metadata["task_id"])
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
