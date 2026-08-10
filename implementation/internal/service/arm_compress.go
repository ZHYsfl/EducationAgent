package service

import (
	"context"
	"fmt"
	"strings"

	"educationagent/internal/state"

	"github.com/openai/openai-go/v3"
)

// Arm history compression, mirroring the voice-side design
// (voice_agent_service.go): a sliding tail window would shift the context
// prefix every turn (breaking KV-cache reuse), can split message triples
// (assistant tool_call → role=tool result → <queue_status> status bar), and
// silently drops task parameters. Instead, once the stored history grows past
// armCompressThreshold, the oldest chunk is compressed into a rolling summary.
//
// Unlike the voice side (which keeps the summary in AppState and injects it
// ephemerally per turn), the arm summary lives INSIDE the stored history as
// one frozen user message (armSummaryMarker prefix) right after the system
// message: the arm loop stores whatever runTurn returns, and an in-history
// summary needs no ephemeral injection. It stays byte-identical between
// compression events, so the context prefix is stable and append-only —
// server-side prefix caching (vLLM APC / SGLang RadixAttention) can hit on
// every turn, missing only on rare compression events.
const (
	// armCompressThreshold triggers compression when the stored arm history
	// exceeds this many messages. A standard grab-place chain is ~17–21
	// messages, so this holds roughly two full task chains.
	armCompressThreshold = 48
	// armCompressKeepRecent is how many recent messages stay verbatim —
	// always more than one full chain, so the current task's context is
	// never summarized away mid-execution.
	armCompressKeepRecent = 24
	// armSummaryMarker prefixes the frozen summary user message stored in
	// the arm history (right after the system message).
	armSummaryMarker = "【此前执行摘要】"
)

// armSummaryPrompt rolls the previous summary and a chunk of old history
// into a new compact summary.
const armSummaryPrompt = `你是执行历史压缩器。把"此前的摘要"和"一段较旧的机械臂执行历史"合并压缩为一段新摘要，要求：
- 保留任务关键参数（物块颜色、目标坐标）、已完成任务的最终结果、未完成事项、来自 voice agent 的未处理指令（改颜色/改位置/取消）；
- 丢弃重复的中间坐标上报、已无关紧要的中间过程；
- 150 字以内，只输出摘要文本。`

// compressArmHistoryIfNeeded compresses older arm turns into the rolling
// summary when the stored history exceeds armCompressThreshold. Compression
// failure is non-fatal: the history is left untouched and compression is
// retried on a later turn; if the history grows past twice the threshold the
// oldest chunk is hard-dropped as a safety valve.
func (s *ArmService) compressArmHistoryIfNeeded(ctx context.Context, st *state.AppState) {
	history := st.GetArmHistory()

	// Pin the system message (if present) at the head.
	var head []openai.ChatCompletionMessageParamUnion
	rest := history
	if len(rest) > 0 && rest[0].OfSystem != nil {
		head = rest[:1]
		rest = rest[1:]
	}

	// Peel off the frozen summary message from a previous compression (if
	// any); its text seeds the roll-forward prompt.
	previous := ""
	if len(rest) > 0 {
		if u := rest[0].OfUser; u != nil && u.Content.OfString.Valid() &&
			strings.HasPrefix(u.Content.OfString.Value, armSummaryMarker) {
			previous = strings.TrimPrefix(u.Content.OfString.Value, armSummaryMarker)
			rest = rest[1:]
		}
	}

	old, recent := splitCompressionWindow(rest, armCompressThreshold, armCompressKeepRecent)
	if old == nil {
		return // below threshold: leave the stored history untouched
	}

	if previous == "" {
		previous = "无"
	}
	summary, err := s.agent.ChatText(ctx, []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(armSummaryPrompt),
		openai.UserMessage(fmt.Sprintf("此前的摘要：\n%s\n\n较旧的执行历史：\n%s", previous, renderHistoryForSummary(old))),
	})
	if err != nil || strings.TrimSpace(summary) == "" {
		if len(rest) > 2*armCompressThreshold {
			st.SetArmHistory(append(head, recent...)) // safety valve against runaway growth
		}
		return
	}

	newHistory := append(head, openai.UserMessage(armSummaryMarker+strings.TrimSpace(summary)))
	newHistory = append(newHistory, recent...)
	st.SetArmHistory(newHistory)
}
