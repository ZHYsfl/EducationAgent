package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"educationagent/internal/model"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/tools"

	"github.com/openai/openai-go/v3"
)

// PPTService manages the PPT agent runtime, its tools, and queue interactions.
type PPTService struct {
	state         *state.AppState
	runtime       *state.PPTAgentRuntime
	agent         *toolcalling.Agent
	kbService     KBService
	searchService SearchService

	// runChatFn is injectable for testing.
	runChatFn func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error)
	runChatMu sync.RWMutex
}

// NewPPTService creates a new PPT service. If agent is nil, a default agent is built from env vars.
func NewPPTService(
	st *state.AppState,
	agent *toolcalling.Agent,
	kb KBService,
	search SearchService,
) *PPTService {
	svc := &PPTService{
		state:         st,
		runtime:       state.NewPPTAgentRuntime(),
		kbService:     kb,
		searchService: search,
	}
	if agent != nil {
		svc.agent = agent
	} else {
		svc.agent = toolcalling.NewAgent(toolcalling.LLMConfig{
			APIKey:  os.Getenv("OPENAI_API_KEY"),
			Model:   os.Getenv("OPENAI_MODEL"),
			BaseURL: os.Getenv("OPENAI_BASE_URL"),
		})
	}
	svc.runChatFn = svc.agent.Chat
	svc.registerTools()
	return svc
}

// SetRunChatFn allows tests to override the chat loop.
func (s *PPTService) SetRunChatFn(fn func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error)) {
	s.runChatMu.Lock()
	defer s.runChatMu.Unlock()
	s.runChatFn = fn
}

// OnVoiceMessage handles all data sent from the voice agent via send_to_ppt_agent.
//   - First call: finalizes requirements and starts the PPT agent runtime.
//   - Subsequent calls while runtime is running: only enqueues data; the running
//     goroutine will notice the queue on its next loop iteration.
//   - Subsequent calls while runtime is stopped: drains the queue into history
//     and starts the runtime again.
func (s *PPTService) OnVoiceMessage(data string) error {
	if !s.state.IsRequirementsFinalized() {
		s.state.MarkRequirementsFinalized()
		req := s.state.GetRequirements()
		s.startRuntime(req, data)
		return nil
	}

	s.state.SendToPPTAgent(data)
	if !s.runtime.IsRunning() {
		s.startRuntimeWithQueuedMessages()
	}
	return nil
}

// SendToVoiceAgent enqueues a message from the PPT agent into the ppt message queue.
func (s *PPTService) SendToVoiceAgent(data string) error {
	s.state.SendToVoiceAgent(data)
	return nil
}

// IsRuntimeRunning reports whether the PPT agent goroutine is active.
func (s *PPTService) IsRuntimeRunning() bool {
	return s.runtime.IsRunning()
}

// logTool wraps a tool function to broadcast call/result to PPT log subscribers.
func (s *PPTService) logTool(name string, fn toolcalling.ToolFunc) toolcalling.ToolFunc {
	return func(ctx context.Context, args map[string]any) (string, error) {
		argsJSON, _ := json.Marshal(args)
		s.state.BroadcastPPTLog(fmt.Sprintf("[tool] %s %s", name, string(argsJSON)))
		result, err := fn(ctx, args)
		if err != nil {
			s.state.BroadcastPPTLog(fmt.Sprintf("[tool_error] %s: %v", name, err))
		} else {
			s.state.BroadcastPPTLog(fmt.Sprintf("[tool_result] %s: %s", name, result))
		}
		s.state.BroadcastPPTLog("[thinking]")
		return result, err
	}
}

// registerTools wires all PPT agent tools into the underlying agent.
func (s *PPTService) registerTools() {
	resolvePath := func(path string) string {
		if len(path) > 0 && path[0] == '/' {
			return path
		}
		return s.state.GetPPTWorkDir() + "/" + path
	}

	s.agent.AddTool(toolcalling.Tool{
		Name:        "send_to_voice_agent",
		Description: "Send a message to the voice agent.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data": map[string]any{"type": "string"},
			},
			"required": []any{"data"},
		},
		Function: s.logTool("send_to_voice_agent", func(ctx context.Context, args map[string]any) (string, error) {
			data, _ := args["data"].(string)
			return s.sendToVoiceAgentTool(ctx, data)
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "fetch_from_voice_message_queue",
		Description: "Fetch the next pending message from the voice message queue, if any.",
		Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Function: s.logTool("fetch_from_voice_message_queue", func(ctx context.Context, _ map[string]any) (string, error) {
			return s.fetchFromVoiceMessageQueueTool(ctx)
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "edit_file",
		Description: "Edit a file by replacing an exact string.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string"},
				"old_string": map[string]any{"type": "string"},
				"new_string": map[string]any{"type": "string"},
			},
			"required": []any{"path", "old_string", "new_string"},
		},
		Function: s.logTool("edit_file", func(ctx context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			oldStr, _ := args["old_string"].(string)
			newStr, _ := args["new_string"].(string)
			if err := tools.EditFile(ctx, resolvePath(path), oldStr, newStr); err != nil {
				return "", err
			}
			return "file edited successfully", nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "write_file",
		Description: "Write (overwrite) content to a file.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []any{"path", "content"},
		},
		Function: s.logTool("write_file", func(ctx context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			if err := tools.WriteFile(ctx, resolvePath(path), content); err != nil {
				return "", err
			}
			return "file written successfully", nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "read_file",
		Description: "Read the full contents of a file.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []any{"path"},
		},
		Function: s.logTool("read_file", func(ctx context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			return tools.ReadFile(ctx, resolvePath(path))
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "list_dir",
		Description: "List the names of entries in a directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string"},
			},
			"required": []any{"path"},
		},
		Function: s.logTool("list_dir", func(ctx context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			entries, err := tools.ListDir(ctx, resolvePath(path))
			if err != nil {
				return "", err
			}
			b, _ := json.Marshal(entries)
			return string(b), nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "move_file",
		Description: "Move (rename) a file from src to dst.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"src": map[string]any{"type": "string"},
				"dst": map[string]any{"type": "string"},
			},
			"required": []any{"src", "dst"},
		},
		Function: s.logTool("move_file", func(ctx context.Context, args map[string]any) (string, error) {
			src, _ := args["src"].(string)
			dst, _ := args["dst"].(string)
			if err := tools.MoveFile(ctx, resolvePath(src), resolvePath(dst)); err != nil {
				return "", err
			}
			return "file moved successfully", nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "execute_command",
		Description: "Execute a shell command in a working directory.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
				"workdir": map[string]any{"type": "string"},
			},
			"required": []any{"command"},
		},
		Function: s.logTool("execute_command", func(ctx context.Context, args map[string]any) (string, error) {
			cmd, _ := args["command"].(string)
			workdir, _ := args["workdir"].(string)
			if workdir == "" {
				workdir = s.state.GetPPTWorkDir()
			}
			stdout, _, err := tools.ExecuteCommand(ctx, cmd, workdir)
			if err != nil {
				return "", err
			}
			return stdout, nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "query_chunks",
		Description: "Query the knowledge base for relevant chunks.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		},
		Function: s.logTool("query_chunks", func(ctx context.Context, args map[string]any) (string, error) {
			query, _ := args["query"].(string)
			chunks, total, err := s.kbService.QueryChunks(ctx, query)
			if err != nil {
				return "", err
			}
			out, _ := json.Marshal(map[string]any{"chunks": chunks, "total": total})
			return string(out), nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name:        "search_web",
		Description: "Search the web and return a summary.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
			"required": []any{"query"},
		},
		Function: s.logTool("search_web", func(ctx context.Context, args map[string]any) (string, error) {
			query, _ := args["query"].(string)
			return s.searchService.SearchWeb(ctx, query)
		}),
	})
}

func (s *PPTService) sendToVoiceAgentTool(_ context.Context, data string) (string, error) {
	s.state.SendToVoiceAgent(data)
	return "data is sent to the voice agent successfully", nil
}

func (s *PPTService) fetchFromVoiceMessageQueueTool(_ context.Context) (string, error) {
	msg, ok := s.state.FetchFromVoiceMessageQueue()
	if !ok {
		return "queue is empty", nil
	}
	return msg, nil
}

func (s *PPTService) buildSystemMessage() openai.ChatCompletionMessageParamUnion {
	queueLen := s.state.VoiceMessageQueueLen()
	queueStatus := "empty"
	if queueLen > 0 {
		queueStatus = fmt.Sprintf("has %d pending message(s)", queueLen)
	}
	content := fmt.Sprintf(
		"You are a PPT generation agent. Use the available tools to create the presentation. "+
			"You must write the slide content to a Markdown file (e.g. slides.md) using Slidev syntax. "+
			"IMPORTANT: Always use `theme: default` in the frontmatter — never use any other theme. "+
			"IMPORTANT: If the content contains Chinese characters, add the following to the frontmatter to avoid garbled text in PDF: "+
			"`fonts:\\n  sans: 'Noto Sans SC'\\n  serif: 'Noto Serif SC'\\n  mono: 'Fira Code'`. "+
			"Before exporting, run `npm init -y && npm install @slidev/cli @slidev/theme-default` to ensure dependencies are installed. " +
			"Also run `apt-get install -y libgbm1 libasound2 2>/dev/null || true` to ensure system libraries are available. "+
			"Then run `npx slidev export slides.md --output ppt.pdf` to produce the final PDF. "+
			"After the PDF is successfully exported, you MUST call send_to_voice_agent to notify the voice agent. "+
			"Current voice message queue status: %s. "+
			"If the queue has messages, call fetch_from_voice_message_queue to consume them.",
		queueStatus,
	)
	return openai.SystemMessage(content)
}

func (s *PPTService) refreshSystemMessageInHistory() []openai.ChatCompletionMessageParamUnion {
	history := s.state.GetPPTHistory()
	sys := s.buildSystemMessage()
	if len(history) > 0 && history[0].OfSystem != nil {
		history[0] = sys
		return history
	}
	return append([]openai.ChatCompletionMessageParamUnion{sys}, history...)
}

func (s *PPTService) startRuntime(req model.Requirements, initialData string) {
	// Set workdir based on topic
	topic := "ppt"
	if req.Topic != nil && *req.Topic != "" {
		topic = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r > 127 {
				return r
			}
			if r == ' ' {
				return '-'
			}
			return -1
		}, *req.Topic)
		if topic == "" {
			topic = "ppt"
		}
	}
	workDir := "/root/autodl-tmp/workspace/" + topic
	s.state.SetPPTWorkDir(workDir)
	_ = os.MkdirAll(workDir, 0755)

	data := initialData
	if data == "" {
		data = fmt.Sprintf(
			"Generate a PPT. Topic: %s, Style: %s, Total Pages: %d, Audience: %s",
			*req.Topic, *req.Style, *req.TotalPages, *req.Audience,
		)
	}
	history := []openai.ChatCompletionMessageParamUnion{
		s.buildSystemMessage(),
		openai.UserMessage(data),
	}
	s.state.SetPPTHistory(history)
	s.runPPTAgentLoop(false)
}

func (s *PPTService) startRuntimeWithQueuedMessages() {
	msgs := s.state.DrainVoiceMessageQueue()
	for _, m := range msgs {
		s.state.AppendPPTHistory(openai.UserMessage(m))
	}
	// Refresh the system message so it reflects the now-empty queue before
	// the first inference of this restart.
	history := s.refreshSystemMessageInHistory()
	s.state.SetPPTHistory(history)
	s.runPPTAgentLoop(true)
}

func (s *PPTService) runPPTAgentLoop(skipFirstRefresh bool) {
	s.runtime.Start(func(ctx context.Context) {
		first := skipFirstRefresh
		for {
			if ctx.Err() != nil {
				return
			}

			var history []openai.ChatCompletionMessageParamUnion
			if first {
				history = s.state.GetPPTHistory()
				first = false
			} else {
				history = s.refreshSystemMessageInHistory()
			}

			// Truncate to keep system message + last 20 turns to avoid context overflow.
			if len(history) > 21 {
				history = append(history[:1], history[len(history)-20:]...)
			}

			s.runChatMu.RLock()
			fn := s.runChatFn
			s.runChatMu.RUnlock()

			msgs, err := fn(ctx, history)
			if err != nil {
				return
			}
			s.state.SetPPTHistory(msgs)

			// Broadcast the latest assistant text to log subscribers.
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].OfAssistant != nil {
					text := msgs[i].OfAssistant.Content.OfString
					if text.Valid() && text.Value != "" {
						s.state.BroadcastPPTLog("[agent] " + text.Value)
					}
					break
				}
			}

			// Keep the runtime alive as long as the queue is not empty,
			// so the agent can decide on its own when to fetch.
			if s.state.VoiceMessageQueueLen() == 0 {
				return
			}
		}
	})
}

// StopRuntime cancels the PPT agent runtime goroutine.
func (s *PPTService) StopRuntime() {
	s.runtime.Stop()
}

// WaitRuntime blocks until the PPT agent runtime goroutine exits.
func (s *PPTService) WaitRuntime() {
	s.runtime.Wait()
}
