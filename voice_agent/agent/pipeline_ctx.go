package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"voiceagent/internal/types"
)

func (p *Pipeline) ttsWorker(ctx context.Context, sentenceCh <-chan string) {
	for {
		select {
		case sentence, ok := <-sentenceCh:
			if !ok {
				return
			}

			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}

			log.Printf("TTS: %s", truncate(sentence, 60))

			audioCh, err := p.ttsClient.Synthesize(ctx, sentence, p.adaptive.Get("tts_chunk_ch"))
			if err != nil {
				log.Printf("tts synthesize: %v", err)
				continue
			}
			for chunk := range audioCh {
				if ctx.Err() != nil {
					return
				}
				p.session.SendAudio(chunk)
			}

		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) launchAsyncContextQueries(ctx context.Context, query string) {
	if p.clients == nil {
		return
	}

	userID := p.session.UserID
	sessionID := p.session.SessionID

	var kbTopScoreMu sync.Mutex
	kbTopScore := 0.0 // KB 没结果时 score=0，应触发搜索结果沉淀
	kbScoreReady := make(chan struct{})
	var kbScoreReadyOnce sync.Once
	markKBReady := func() {
		kbScoreReadyOnce.Do(func() { close(kbScoreReady) })
	}

	p.asyncQuery(ctx, "knowledge_base", "rag_chunks", func() (string, error) {
		defer markKBReady()
		resp, err := p.clients.QueryKB(ctx, KBQueryRequest{
			UserID:         userID,
			Query:          query,
			TopK:           5,
			ScoreThreshold: 0.5,
		})
		if err != nil {
			return "", err
		}
		if len(resp.Chunks) > 0 {
			kbTopScoreMu.Lock()
			kbTopScore = resp.Chunks[0].Score
			kbTopScoreMu.Unlock()
		}
		return formatChunksForLLM(resp.Chunks), nil
	})

	p.asyncQuery(ctx, "memory", "memory_recall", func() (string, error) {
		resp, err := p.clients.RecallMemory(ctx, MemoryRecallRequest{
			UserID:    userID,
			SessionID: sessionID,
			Query:     query,
			TopK:      10,
		})
		if err != nil {
			return "", err
		}
		return formatMemoryForLLM(resp), nil
	})

	p.asyncQuery(ctx, "web_search", "search_result", func() (string, error) {
		resp, err := p.clients.SearchWeb(ctx, SearchRequest{
			RequestID:  NewID("search_"),
			UserID:     userID,
			Query:      query,
			MaxResults: 5,
			Language:   "zh",
			SearchType: "general",
		})
		if err != nil {
			return "", err
		}

		// Wait briefly for KB score so ingest decision uses KB result when available.
		select {
		case <-kbScoreReady:
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}

		kbTopScoreMu.Lock()
		shouldIngest := kbTopScore < 0.5
		kbTopScoreMu.Unlock()
		if shouldIngest && len(resp.Results) > 0 {
			items := make([]SearchIngestItem, 0, len(resp.Results))
			for _, r := range resp.Results {
				items = append(items, SearchIngestItem{
					Title:   r.Title,
					URL:     r.URL,
					Content: r.Snippet,
					Source:  r.Source,
				})
			}
			go func(items []SearchIngestItem) {
				if err := p.clients.IngestFromSearch(context.Background(), IngestFromSearchRequest{
					UserID: userID,
					Items:  items,
				}); err != nil {
					log.Printf("[context-bus] ingest-from-search failed: %v", err)
				}
			}(items)
		}
		return formatSearchForLLM(resp), nil
	})
}

func formatChunksForLLM(chunks []RetrievedChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("知识库检索结果：\n")
	for i, c := range chunks {
		sb.WriteString(fmt.Sprintf("%d) [%s] %s (score=%.2f)\n", i+1, c.DocTitle, c.Content, c.Score))
	}
	return strings.TrimSpace(sb.String())
}

func formatMemoryForLLM(resp MemoryRecallResponse) string {
	var sb strings.Builder
	if len(resp.Facts) > 0 {
		sb.WriteString("相关事实记忆：\n")
		for i, f := range resp.Facts {
			text := strings.TrimSpace(f.Content)
			if text == "" {
				text = strings.TrimSpace(f.Value)
			}
			if text == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, text))
		}
	}
	if len(resp.Preferences) > 0 {
		sb.WriteString("相关偏好：\n")
		for i, f := range resp.Preferences {
			text := strings.TrimSpace(f.Content)
			if text == "" {
				text = strings.TrimSpace(f.Value)
			}
			if text == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, text))
		}
	}
	if resp.ProfileSummary != "" {
		sb.WriteString("画像摘要：")
		sb.WriteString(resp.ProfileSummary)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func formatSearchForLLM(resp SearchResponse) string {
	var sb strings.Builder
	if resp.Summary != "" {
		sb.WriteString("网络搜索摘要：")
		sb.WriteString(resp.Summary)
		sb.WriteString("\n")
	}
	if len(resp.Results) > 0 {
		sb.WriteString("搜索结果：\n")
		for i, r := range resp.Results {
			sb.WriteString(fmt.Sprintf("%d) %s - %s (%s)\n", i+1, r.Title, r.Snippet, r.URL))
		}
	}
	return strings.TrimSpace(sb.String())
}

// EnqueueContext adds a context message to the queue and triggers active push if idle
func (p *Pipeline) EnqueueContext(msg types.ContextMessage) {
	if msg.Priority == "high" {
		select {
		case p.highPriorityQueue <- msg:
		default:
			log.Printf("[ctx] high priority queue full")
		}
	} else {
		select {
		case p.contextQueue <- msg:
		default:
			log.Printf("[ctx] context queue full")
		}
	}

	if p.session.GetState() == StateIdle {
		go p.processContextUpdate(context.Background(), msg)
	}
}

func (p *Pipeline) processContextUpdate(ctx context.Context, msg types.ContextMessage) {
	prompt := fmt.Sprintf("新任务结果（%s）: %s", msg.Source, msg.Content)
	p.startProcessing(ctx, prompt)
}

