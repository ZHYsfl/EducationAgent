//go:build ignore

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "Usage: go run transform.go <src_dir> <dst_dir>")
		os.Exit(1)
	}

	srcDir := os.Args[1]
	dstDir := os.Args[2]

	files, err := filepath.Glob(filepath.Join(srcDir, "*_test.go"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Glob error:", err)
		os.Exit(1)
	}

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Read error:", f, err)
			continue
		}

		transformed := transform(string(data))
		dst := filepath.Join(dstDir, filepath.Base(f))
		if err := os.WriteFile(dst, []byte(transformed), 0644); err != nil {
			fmt.Fprintln(os.Stderr, "Write error:", dst, err)
		} else {
			fmt.Println("OK:", filepath.Base(f))
		}
	}
}

func addImport(content string) string {
	ibr := regexp.MustCompile(`(?s)import \((.*?)\)`)
	if ibr.MatchString(content) {
		return ibr.ReplaceAllStringFunc(content, func(m string) string {
			if !strings.Contains(m, `"voiceagent/agent"`) {
				return strings.Replace(m, "import (", "import (\n\tagent \"voiceagent/agent\"", 1)
			}
			return m
		})
	}
	sir := regexp.MustCompile(`import "([^"]+)"`)
	if sir.MatchString(content) {
		return sir.ReplaceAllStringFunc(content, func(m string) string {
			inner := strings.TrimPrefix(m, "import ")
			return "import (\n\tagent \"voiceagent/agent\"\n\t" + inner + "\n)"
		})
	}
	// No import at all — insert after package line
	return regexp.MustCompile(`(package agent_test\n)`).ReplaceAllString(
		content, "$1\nimport (\n\tagent \"voiceagent/agent\"\n)\n")
}

// prefixBareIdent replaces word occurrences of `ident` that are NOT already
// preceded by `agent.` with `agent.ident`.
func transform(content string) string {
	content = strings.Replace(content, "package agent\n", "package agent_test\n", 1)
	content = addImport(content)

	// -----------------------------------------------------------------------
	// Mock helpers and constructor functions
	// -----------------------------------------------------------------------
	simpleStr := []struct{ old, new string }{
		// Constructors / helpers
		{"&mockServices{}", "&agent.MockServices{}"},
		{"mockServices{}", "agent.MockServices{}"},
		{"newTestSession(", "agent.NewTestSession("},
		{"newTestPipeline(", "agent.NewTestPipeline("},
		{"newTestPipelineWithTTS(", "agent.NewTestPipelineWithTTS("},
		{"newTestConfig()", "agent.NewTestConfig()"},
		{"drainWriteCh(", "agent.DrainWriteCh("},
		{"findWSMessage(", "agent.FindWSMessage("},
		{"waitForFeedback(", "agent.WaitForFeedback("},
		{"waitForExtractMem(", "agent.WaitForExtractMem("},
		// Package-level unexported functions → exported
		{"isSentenceEnd(", "agent.IsSentenceEnd("},
		{"formatProfileSummary(", "agent.FormatProfileSummary("},
		{"formatChunksForLLM(", "agent.FormatChunksForLLM("},
		{"formatMemoryForLLM(", "agent.FormatMemoryForLLM("},
		{"formatSearchForLLM(", "agent.FormatSearchForLLM("},
		{"decodeAPIData(", "agent.DecodeAPIData("},
		{"toReferenceFiles(", "agent.ToReferenceFiles("},
		{"buildDetailedDescription(", "agent.BuildDetailedDescription("},
		// truncate — careful: only the package-level call (not s.GetConfig().truncate etc)
		// We handle truncate as a standalone call below via prefixBareIdent on "truncate"
		// Package-level registry funcs
		{"registerSession(", "agent.RegisterSession("},
		{"unregisterSession(", "agent.UnregisterSession("},
		{"registerTask(", "agent.RegisterTask("},
		{"findSessionByTaskID(", "agent.FindSessionByTaskID("},
		// Pipeline unexported methods → exported wrappers
		{"p.startProcessing(", "p.StartProcessing("},
		{"p.startDraftThinking(", "p.StartDraftThinking("},
		{"p.postProcessResponse(", "p.PostProcessResponse("},
		{"p.tryResolveConflict(", "p.TryResolveConflict("},
		{"p.tryDetectTaskInit(", "p.TryDetectTaskInit("},
		{"p.trySendPPTFeedback(", "p.TrySendPPTFeedback("},
		{"p.handleRequirementsTransition(", "p.HandleRequirementsTransition("},
		{"p.buildTaskListContext(", "p.BuildTaskListContext("},
		{"p.buildPendingQuestionsContext(", "p.BuildPendingQuestionsContext("},
		{"p.drainContextQueue(", "p.DrainContextQueue("},
		{"p.drainASRResults(", "p.DrainASRResults("},
		{"p.asyncQuery(", "p.AsyncQuery("},
		{"p.asyncExtractMemory(", "p.AsyncExtractMemory("},
		{"p.highPriorityListener(", "p.HighPriorityListener("},
		{"p.launchAsyncContextQueries(", "p.LaunchAsyncContextQueries("},
		{"p.getDraftOutput(", "p.GetDraftOutput("},
		{"p.appendDraftOutput(", "p.AppendDraftOutput("},
		{"p.resetDraftOutput(", "p.ResetDraftOutput("},
		{"p.extractContextIDFromResponse(", "p.ExtractContextIDFromResponse("},
		{"p.ttsWorker(", "p.TTSWorker("},
		{"p.cancelDraft(", "p.CancelDraft("},
		{"p.enqueueContextMessage(", "p.EnqueueContextMessage("},
		// Session unexported methods → exported wrappers
		{"s.cancelCurrentPipeline(", "s.CancelCurrentPipeline("},
		{"s.newPipelineContext(", "s.NewPipelineContext("},
		{"s.handleTextMessage(", "s.HandleTextMessage("},
		{"s.handleTextInput(", "s.HandleTextInput("},
		{"s.handlePageNavigate(", "s.HandlePageNavigate("},
		{"s.handleTaskInit(", "s.HandleTaskInit("},
		{"s.handleRequirementsConfirm(", "s.HandleRequirementsConfirm("},
		{"s.onVADEnd(", "s.OnVADEnd("},
		{"s.onVADStart(", "s.OnVADStart("},
		{"s.publishVADEvent(", "s.PublishVADEvent("},
		{"s.createPPTFromRequirements(", "s.CreatePPTFromRequirements("},
		{"s.handleAudioData(", "s.HandleAudioData("},
		{"s.readLoop(", "s.ReadLoop("},
		{"s.writeLoop(", "s.WriteLoop("},
		{"s.speakText(", "s.SpeakText("},
		{"s.prefillFromMemory(", "s.PrefillFromMemory("},
		// Pipeline field mu operations
		{"p.rawGeneratedTokens.WriteString(", "p.WriteRawTokens("},
		{"p.rawGeneratedTokens.Len()", "p.RawTokensLen()"},
		{"p.pendingMu.Lock()", "p.LockPending()"},
		{"p.pendingMu.Unlock()", "p.UnlockPending()"},
		// Session field assignment (exact pattern)
		{"s.pipeline = p", "s.SetPipeline(p)"},
		// var-level type assertions
		{"var _ agent.ExternalServices = (*agent.MockServices)(nil)", ""},
	}
	for _, r := range simpleStr {
		content = strings.ReplaceAll(content, r.old, r.new)
	}

	// -----------------------------------------------------------------------
	// Pipeline field setter assignments (whole-line regex):
	// "    p.field = VALUE" → "    p.SetField(VALUE)"
	// -----------------------------------------------------------------------
	type setterSpec struct {
		field  string
		setter string
	}
	setters := []setterSpec{
		{`p\.ttsClient`, `p.SetTTSClient`},
		{`p\.asrClient`, `p.SetASRClient`},
		{`p\.smallLLM`, `p.SetSmallLLM`},
		{`p\.largeLLM`, `p.SetLargeLLM`},
		{`p\.audioCh`, `p.SetAudioCh`},
		{`p\.vadEndCh`, `p.SetVADEndCh`},
		{`p\.draftCancel`, `p.SetDraftCancel`},
	}
	for _, sp := range setters {
		re := regexp.MustCompile(`(?m)^(\t*)` + sp.field + ` = (.+)$`)
		content = re.ReplaceAllString(content, `${1}`+sp.setter+`(${2})`)
	}

	// s.Requirements = VALUE → s.SetRequirements(VALUE)
	content = regexp.MustCompile(`(?m)^(\t*)s\.Requirements = (.+)$`).ReplaceAllString(
		content, `${1}s.SetRequirements(${2})`)

	// s.state = StateXxx → s.SetStateRaw(agent.StateXxx)
	// NOTE: The state name won't have agent. prefix yet at this point.
	content = regexp.MustCompile(`(?m)^(\t*)s\.state = (\w+)$`).ReplaceAllString(
		content, `${1}s.SetStateRaw(agent.${2})`)

	// -----------------------------------------------------------------------
	// Field READ accessors
	// -----------------------------------------------------------------------
	content = regexp.MustCompile(`\bs\.pipeline\.`).ReplaceAllString(content, "s.GetPipeline().")
	content = regexp.MustCompile(`\bs\.pipeline\b`).ReplaceAllString(content, "s.GetPipeline()")

	content = regexp.MustCompile(`\bp\.audioCh\b`).ReplaceAllString(content, "p.GetAudioCh()")
	content = regexp.MustCompile(`\bp\.vadEndCh\b`).ReplaceAllString(content, "p.GetVADEndCh()")
	content = regexp.MustCompile(`\bp\.highPriorityQueue\b`).ReplaceAllString(content, "p.GetHighPriorityQueue()")
	content = regexp.MustCompile(`\bp\.contextQueue\b`).ReplaceAllString(content, "p.GetContextQueue()")
	content = regexp.MustCompile(`\bp\.pendingContexts\b`).ReplaceAllString(content, "p.GetPendingContexts()")
	content = regexp.MustCompile(`\bp\.history\b`).ReplaceAllString(content, "p.GetHistory()")
	content = regexp.MustCompile(`\bp\.largeLLM\b`).ReplaceAllString(content, "p.GetLargeLLM()")
	content = regexp.MustCompile(`\bp\.smallLLM\b`).ReplaceAllString(content, "p.GetSmallLLM()")
	content = regexp.MustCompile(`\bp\.ttsClient\b`).ReplaceAllString(content, "p.GetTTSClient()")
	content = regexp.MustCompile(`\bp\.asrClient\b`).ReplaceAllString(content, "p.GetASRClient()")
	content = regexp.MustCompile(`\bp\.config\b`).ReplaceAllString(content, "p.GetConfig()")
	content = regexp.MustCompile(`\bp\.adaptive\b`).ReplaceAllString(content, "p.GetAdaptiveController()")
	content = regexp.MustCompile(`\bp\.draftCancel\b`).ReplaceAllString(content, "p.GetDraftCancel()")

	content = regexp.MustCompile(`\bs\.pipelineCancel\b`).ReplaceAllString(content, "s.GetPipelineCancel()")
	content = regexp.MustCompile(`\bs\.Requirements\b`).ReplaceAllString(content, "s.GetRequirements()")
	content = regexp.MustCompile(`\bs\.clients\b`).ReplaceAllString(content, "s.GetClients()")
	content = regexp.MustCompile(`\bs\.config\b`).ReplaceAllString(content, "s.GetConfig()")
	content = regexp.MustCompile(`\bs\.writeCh\b`).ReplaceAllString(content, "s.GetWriteCh()")
	content = regexp.MustCompile(`\bs\.state\b`).ReplaceAllString(content, "s.GetState()")

	// -----------------------------------------------------------------------
	// Type names that need agent. prefix when used as composite literals,
	// slice types, pointer types, or standalone type references.
	// We use prefixBareIdent which only adds the prefix when not already present.
	// -----------------------------------------------------------------------
	typeNames := []string{
		"ContextMessage",
		"WSMessage",
		"TaskRequirements",
		"UserProfile",
		"KBQueryRequest",
		"KBQueryResponse",
		"MemoryRecallRequest",
		"MemoryRecallResponse",
		"SearchRequest",
		"SearchResponse",
		"PPTInitRequest",
		"PPTInitResponse",
		"PPTFeedbackRequest",
		"CanvasStatusResponse",
		"IngestFromSearchRequest",
		"MemoryExtractRequest",
		"MemoryExtractResponse",
		"WorkingMemorySaveRequest",
		"WorkingMemory",
		"VADEvent",
		"RetrievedChunk",
		"ReferenceFile",
		"ReferenceFileReq",
		"PageInfoBrief",
		"PPTMessageRequest",
		"ExternalServices",
		"SessionState",
		"writeItem",
	}
	for _, t := range typeNames {
		content = prefixBareIdent(content, t)
	}

	// State constants
	for _, state := range []string{
		"StateIdle", "StateListening", "StateProcessing", "StateSpeaking", "StateRequirements",
	} {
		content = prefixBareIdent(content, state)
	}

	// NewID function
	content = strings.ReplaceAll(content, "NewID(", "agent.NewID(")

	// -----------------------------------------------------------------------
	// truncate( standalone — the function name conflicts with strings.Truncate etc.
	// Only matches bare truncate( not already prefixed.
	// -----------------------------------------------------------------------
	content = regexp.MustCompile(`([^.a-zA-Z_])truncate\(`).ReplaceAllString(content, `$1agent.Truncate(`)

	// -----------------------------------------------------------------------
	// Fix double agent. prefix from multiple replacement passes
	// -----------------------------------------------------------------------
	for i := 0; i < 5; i++ {
		content = strings.ReplaceAll(content, "agent.agent.", "agent.")
	}

	// -----------------------------------------------------------------------
	// Remove artifacts
	// -----------------------------------------------------------------------
	content = strings.ReplaceAll(content, "_ = agent.MockServices{}", "")

	// Fix: "agent.NewID(agent.NewID(" → only one prefix (if NewID appears twice)
	// Already covered by double-prefix fix above.

	// Fix: "var _ agent.agent.ExternalServices" etc. already covered.

	return content
}

// prefixBareIdent adds "agent." prefix to occurrences of `ident` that are NOT
// already preceded by "agent." (or any other dot-qualified prefix).
func prefixBareIdent(content, ident string) string {
	result := &strings.Builder{}
	result.Grow(len(content))
	remaining := content
	prefix := "agent."
	for {
		idx := strings.Index(remaining, ident)
		if idx < 0 {
			result.WriteString(remaining)
			break
		}
		// Check that ident ends at a word boundary (next char is not ident char)
		afterIdx := idx + len(ident)
		if afterIdx < len(remaining) && isIdentChar(remaining[afterIdx]) {
			// Part of a longer identifier — skip past this occurrence
			result.WriteString(remaining[:afterIdx])
			remaining = remaining[afterIdx:]
			continue
		}
		// Check that we're not already prefixed with "agent." or any other ".xxx"
		alreadyQualified := false
		if idx >= 1 && remaining[idx-1] == '.' {
			alreadyQualified = true
		}
		if !alreadyQualified && idx >= len(prefix) && remaining[idx-len(prefix):idx] == prefix {
			alreadyQualified = true
		}
		// Also skip if preceded by a regular identifier char (not a dot) — means it's
		// part of a compound name like "mockContextMessage"
		if !alreadyQualified && idx >= 1 && isIdentCharNoDot(remaining[idx-1]) {
			alreadyQualified = true
		}

		result.WriteString(remaining[:idx])
		if !alreadyQualified {
			result.WriteString(prefix)
		}
		result.WriteString(ident)
		remaining = remaining[afterIdx:]
	}
	return result.String()
}

func isIdentChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '.'
}

func isIdentCharNoDot(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}
