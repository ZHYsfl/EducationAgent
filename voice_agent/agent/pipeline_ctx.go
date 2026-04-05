package agent

import (
	"context"
	"fmt"
	"log"
	"strings"

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

// EnqueueContext adds a context message to the queue and triggers active push if idle
func (p *Pipeline) EnqueueContext(msg types.ContextMessage) {
	// Handle requirements updates immediately
	if msg.MsgType == "requirements_updated" {
		p.handleRequirementsUpdate(msg.Content)
		return
	}

	// Handle task_list_update: register task and notify frontend
	if msg.MsgType == "task_list_update" {
		taskID := msg.Metadata["task_id"]
		topic := msg.Metadata["topic"]
		if taskID != "" {
			p.session.RegisterTask(taskID, topic)
			p.session.SetActiveTask(taskID)
			p.session.SendJSON(WSMessage{
				Type:         "task_list_update",
				ActiveTaskID: taskID,
				Tasks:        p.session.GetOwnedTasks(),
			})
		}
		return
	}

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
	prompt := fmt.Sprintf("新任务结果（%s）: %s", msg.ActionType, msg.Content)
	p.startProcessing(ctx, prompt)
}
