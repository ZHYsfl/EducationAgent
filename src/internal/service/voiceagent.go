package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"agent_runtime"
	"educationagent/internal/model"
	"educationagent/internal/state"
	"educationagent/internal/voiceagent"

	"github.com/openai/openai-go/v3"
)

const (
	phase1SystemPrompt = `You are a voice assistant focused on helping users create PPTs, currently in the requirement collection phase (Phase 1). The PPT Agent has not yet started.

Objective:
Through natural and friendly conversation, collect the following 4 required fields from the user:
1. topic
2. style
3. total_pages
4. audience

You have 3 tools:
1. update_requirements, used to update collected fields. The tool returns the remaining missing field names, or returns "all fields are updated".
2. require_confirm, only used after all 4 fields have been collected. The tool returns "data is sent to the frontend successfully".
3. send_to_ppt_agent, only used after the user confirms the requirements are correct, used to send the requirements to the PPT Agent to officially start generation. Once this action is executed, Phase 1 permanently ends and enters Phase 2.

Iron rules:
1. During Phase 1, the queue_status of the ppt_messages_queue for messages sent to you by the ppt agent is always empty; you do not need to pay attention to this queue's status information, just focus on requirement collection.
2. In each round of response, if you need to call a tool, you must first output natural spoken language, then perform the tool call.
3. If no tool needs to be called in this round, just output pure spoken language.
4. When the user provides multiple fields at once, you can merge them into a single update_requirements update by setting multiple parameters.
5. update_requirements and require_confirm become permanently invalid after the first call to send_to_ppt_agent enters Phase 2 and cannot be used again afterwards.
6. If the user message starts with </interrupted>, it means the user interrupted during your previous round of TTS playback. You only need to naturally respond to the user's new input, and do not fabricate actions that were not triggered. And you should have the ability to make a deferred call: if in a previous round you intended to call a certain tool but were interrupted too early to call it, seize the opportunity and make the deferred call this time. Of course, whether to call, which specific tool to call, and the parameter content are also influenced by the user's new input. You should analyze and weigh according to the specific situation.`

	phase2SystemPrompt = `You are a voice assistant, currently acting as the communication bridge between the user and the PPT Agent.

Responsibilities:
1. Naturally chat with the user about life, or answer questions related to PPT.
2. When the user message contains <queue_status>not empty</queue_status>, proactively call get_messages_from_ppt_agent to pull messages from the PPT Message Queue.
3. Forward user feedback, replies, or new instructions to the PPT Agent via send_to_ppt_agent. What information should be forwarded, what should not be changed, and what should be further clarified with the user before sending — these are for you to decide and weigh.
4. Report messages returned by the PPT Agent to the user in natural language.

You have 2 tools:
1. get_messages_from_ppt_agent, used when the user message queue_status is not empty, to pull queue messages and obtain information sent from the ppt agent.
2. send_to_ppt_agent, selectively forwards user feedback, replies, or new instructions to the PPT Agent after your filtering, processing, and handling.

Iron rules:
1. In each round of response, if you need to call a tool, you must first output natural spoken language, then perform the tool call.
2. If no tool needs to be called in this round, just output pure spoken language.
3. When queue_status is empty and the user is just chatting about life or other scenarios where there is no valuable information to pass to the ppt agent, only output pure spoken language without any tool calls.
4. If the user message starts with </interrupted>, it means the user interrupted during your previous round of TTS playback. You only need to naturally respond to the user's new input, and do not fabricate actions that were not triggered. And you should have the ability to make a deferred call: if in a previous round you intended to call a certain tool but were interrupted too early to call it, seize the opportunity and make the deferred call this time. Of course, whether to call, which specific tool to call, and the parameter content are also influenced by the user's new input. You should analyze and weigh according to the specific situation.`
)

// VoiceAgentService drives the LLM loop for the voice agent.
type VoiceAgentService struct {
	agent *agent_runtime.Agent
	exec  *voiceagent.Executor
}

// NewVoiceAgentService creates a new voice agent runtime service.
func NewVoiceAgentService(
	cfg agent_runtime.LLMConfig,
	exec *voiceagent.Executor,
	opts ...agent_runtime.AgentOption,
) *VoiceAgentService {
	agent := agent_runtime.NewAgent(&cfg, nil, opts...)
	return &VoiceAgentService{agent: agent, exec: exec}
}

// StreamTurn runs one assistant turn and streams chunks to out.
// It assumes the caller has already serialized turns via state.LockVoiceTurn.
func (s *VoiceAgentService) StreamTurn(
	ctx context.Context,
	store *state.AppState,
	segments []string,
	needsInterruptedPrefix bool,
	interruptedAssistantText string,
	out chan<- model.SSEChunk,
) error {
	store.SetCurrentTurnActive(true)
	defer store.SetCurrentTurnActive(false)

	if interruptedAssistantText != "" {
		store.AppendVoiceHistory(assistantMessage(interruptedAssistantText))
	}

	pendingResults := store.GetPendingToolResults()
	userMsgs := assembleUserMessages(store, segments, needsInterruptedPrefix, pendingResults)

	transcript := renderUserMessages(userMsgs)
	sendChunk(ctx, out, model.SSEChunk{Type: "user_transcript", Text: transcript})

	for _, m := range userMsgs {
		store.AppendVoiceHistory(m)
	}

	enteredToolCallPhase := false
	var assistantSpoken strings.Builder
	var toolCalls []voiceagent.ToolCall
	var toolResults []state.ToolResult

	systemPrompt := buildSystemPrompt(store, s.exec.ToolSchemas())
	messages := prependMessage(store.GetVoiceHistory(), systemMessage(systemPrompt))

	stream := s.agent.ChatWithoutToolCallStream(ctx, messages)
	parser := voiceagent.NewStreamParser()

	for token := range stream {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		spoken, calls := parser.Feed(token)
		if spoken != "" {
			assistantSpoken.WriteString(spoken)
			sendChunk(ctx, out, model.SSEChunk{Type: "tts", Text: spoken})
		}
		if len(calls) > 0 {
			enteredToolCallPhase = true
			store.SetCurrentTurnEnteredToolCallPhase(true)
			toolCalls = append(toolCalls, calls...)
			results, err := s.executeToolCalls(ctx, calls, out)
			if err != nil {
				return err
			}
			toolResults = append(toolResults, results...)
		}
	}

	if spoken := parser.Flush(); spoken != "" {
		assistantSpoken.WriteString(spoken)
		sendChunk(ctx, out, model.SSEChunk{Type: "tts", Text: spoken})
	}

	// Persist assistant message (spoken text + raw tool call blocks).
	assistantContent := buildAssistantContent(assistantSpoken.String(), toolCalls)
	if assistantContent != "" {
		store.AppendVoiceHistory(assistantMessage(assistantContent))
	}

	store.SetPreviousTurnEnteredToolCallPhase(enteredToolCallPhase)

	if len(toolCalls) > 0 {
		if store.GetWaitingEpisode() != nil {
			// Interrupted scenarios 2/3: keep tool results pending for the next turn.
			store.MarkToolCallDone(toolResults)
		} else {
			// Normal flow: run report pass.
			if err := s.runReportPass(ctx, store, toolResults, out); err != nil {
				return err
			}
		}
	}

	sendChunk(ctx, out, model.SSEChunk{Type: "turn_end"})
	return nil
}

// executeToolCalls runs the provided tool calls concurrently and emits a
// tool_call chunk immediately, followed by the matching tool_response chunks
// in the original order once all calls have finished.
func (s *VoiceAgentService) executeToolCalls(
	ctx context.Context,
	calls []voiceagent.ToolCall,
	out chan<- model.SSEChunk,
) ([]state.ToolResult, error) {
	slots := make([]state.ToolResult, len(calls))
	var wg sync.WaitGroup

	for i, tc := range calls {
		payload, _ := json.Marshal(map[string]any{
			"name":      tc.Name,
			"arguments": tc.Arguments,
		})
		sendChunk(ctx, out, model.SSEChunk{Type: "tool_call", Payload: string(payload)})

		wg.Add(1)
		go func(idx int, call voiceagent.ToolCall) {
			defer wg.Done()
			name, result, err := s.exec.Execute(ctx, call)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			slots[idx] = state.ToolResult{Name: name, Result: result}
		}(i, tc)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	for i := range slots {
		sendChunk(ctx, out, model.SSEChunk{Type: "tool_response", Text: slots[i].Result})
	}
	return slots, nil
}

func (s *VoiceAgentService) runReportPass(
	ctx context.Context,
	store *state.AppState,
	results []state.ToolResult,
	out chan<- model.SSEChunk,
) error {
	var tb strings.Builder
	for _, r := range results {
		tb.WriteString("<tool_response>\n")
		tb.WriteString(r.Result)
		tb.WriteString("\n</tool_response>\n")
	}
	reportUserMsg := openai.UserMessage(tb.String())
	store.AppendVoiceHistory(reportUserMsg)

	systemPrompt := buildSystemPrompt(store, s.exec.ToolSchemas())
	messages := prependMessage(store.GetVoiceHistory(), systemMessage(systemPrompt))

	stream := s.agent.ChatWithoutToolCallStream(ctx, messages)
	parser := voiceagent.NewStreamParser()
	var reportSpoken strings.Builder

	for token := range stream {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		spoken, calls := parser.Feed(token)
		if spoken != "" {
			reportSpoken.WriteString(spoken)
			sendChunk(ctx, out, model.SSEChunk{Type: "tts", Text: spoken})
		}
		_ = calls // report pass should not contain tool calls per api.md.
	}
	if spoken := parser.Flush(); spoken != "" {
		reportSpoken.WriteString(spoken)
		sendChunk(ctx, out, model.SSEChunk{Type: "tts", Text: spoken})
	}
	if reportSpoken.String() != "" {
		store.AppendVoiceHistory(assistantMessage(reportSpoken.String()))
	}
	return nil
}

func assembleUserMessages(
	store *state.AppState,
	segments []string,
	needsInterruptedPrefix bool,
	pendingResults []state.ToolResult,
) []openai.ChatCompletionMessageParamUnion {
	var msgs []openai.ChatCompletionMessageParamUnion

	if len(pendingResults) > 0 {
		var tb strings.Builder
		for _, r := range pendingResults {
			tb.WriteString("<tool_response>\n")
			tb.WriteString(r.Result)
			tb.WriteString("\n</tool_response>\n")
		}
		msgs = append(msgs, openai.UserMessage(tb.String()))
	}

	for i, seg := range segments {
		saying := seg
		if needsInterruptedPrefix && i == 0 {
			saying = "</interrupted>" + saying
		}
		msgs = append(msgs, openai.UserMessage(saying))
	}

	queueStatus := "empty"
	if store.IsRequirementsFinalized() {
		if store.PPTToVoiceQueueLen() > 0 {
			queueStatus = "not empty"
		}
	}
	msgs = append(msgs, openai.UserMessage(fmt.Sprintf("<queue_status>%s</queue_status>", queueStatus)))

	return msgs
}

func buildSystemPrompt(store *state.AppState, schemas []voiceagent.ToolSchema) string {
	base := phase1SystemPrompt
	if store.IsRequirementsFinalized() {
		base = phase2SystemPrompt
	}

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n\n# Tools\n\nYou may call one or more functions to assist with the user query.\n\nYou are provided with function signatures within <tools></tools> XML tags:\n<tools>")
	for _, sc := range schemas {
		b, _ := json.Marshal(sc)
		sb.WriteString("\n")
		sb.Write(b)
	}
	sb.WriteString("\n</tools>\n\nFor each function call, return a json object with function name and arguments within <tool_call></tool_call> XML tags:\n<tool_call>\n{\"name\": <function-name>, \"arguments\": <args-json-object>}\n</tool_call>")
	return sb.String()
}

func renderUserMessages(msgs []openai.ChatCompletionMessageParamUnion) string {
	var sb strings.Builder
	for _, m := range msgs {
		content := ""
		if m.OfUser != nil {
			if m.OfUser.Content.OfString.Valid() {
				content = m.OfUser.Content.OfString.Value
			} else {
				for _, p := range m.OfUser.Content.OfArrayOfContentParts {
					if p.OfText != nil {
						content += p.OfText.Text
					}
				}
			}
		}
		sb.WriteString("<|im_start|>user\n")
		sb.WriteString(content)
		sb.WriteString("<|im_end|>\n")
	}
	return sb.String()
}

func buildAssistantContent(spoken string, toolCalls []voiceagent.ToolCall) string {
	var sb strings.Builder
	if spoken != "" {
		sb.WriteString(spoken)
	}
	for _, tc := range toolCalls {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		b, _ := json.Marshal(map[string]any{
			"name":      tc.Name,
			"arguments": tc.Arguments,
		})
		sb.WriteString("<tool_call>\n")
		sb.Write(b)
		sb.WriteString("\n</tool_call>")
	}
	return sb.String()
}

func prependMessage(msgs []openai.ChatCompletionMessageParamUnion, head openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(msgs)+1)
	out = append(out, head)
	out = append(out, msgs...)
	return out
}

func sendChunk(ctx context.Context, out chan<- model.SSEChunk, chunk model.SSEChunk) {
	select {
	case out <- chunk:
	case <-ctx.Done():
	}
}

func systemMessage(content string) openai.ChatCompletionMessageParamUnion {
	return openai.ChatCompletionMessageParamUnion{
		OfSystem: &openai.ChatCompletionSystemMessageParam{
			Content: openai.ChatCompletionSystemMessageParamContentUnion{
				OfString: openai.String(content),
			},
		},
	}
}

func assistantMessage(content string) openai.ChatCompletionMessageParamUnion {
	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			Content: openai.ChatCompletionAssistantMessageParamContentUnion{
				OfString: openai.String(content),
			},
		},
	}
}
