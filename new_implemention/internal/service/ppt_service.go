package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"educationagent/internal/model"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/tools"

	"github.com/openai/openai-go/v3"
)

const (
	slidevNPMInstallTimeout = 15 * time.Minute
	// slidevSharedDepsDir holds one npm install shared by all topic dirs via symlink (no per-project network).
	slidevSharedDepsDir = "/root/autodl-tmp/workspace/.slidev-shared"
)

// slidevPackageJSON must match what we seed in each topic workdir.
const slidevPackageJSON = `{
  "name": "math-presentation",
  "version": "1.0.0",
  "scripts": {
    "export": "slidev export slides.md --output ppt.pdf"
  },
  "dependencies": {
    "@slidev/cli": "latest",
    "@slidev/theme-default": "latest",
    "@slidev/theme-seriph": "latest",
    "@slidev/theme-bricks": "latest",
    "@slidev/theme-shibainu": "latest",
    "@slidev/theme-apple-basic": "latest",
    "playwright-chromium": "latest"
  }
}`

// slidevSharedRequiredSubdirs are paths under node_modules; if any are missing, shared cache is refreshed with npm install.
var slidevSharedRequiredSubdirs = []string{
	filepath.Join("@slidev", "cli"),
	filepath.Join("@slidev", "theme-default"),
	filepath.Join("@slidev", "theme-seriph"),
	filepath.Join("@slidev", "theme-bricks"),
	filepath.Join("@slidev", "theme-shibainu"),
	filepath.Join("@slidev", "theme-apple-basic"),
	"playwright-chromium",
}

func slidevSharedCacheIncomplete(nodeModulesDir string) bool {
	for _, sub := range slidevSharedRequiredSubdirs {
		if _, err := os.Stat(filepath.Join(nodeModulesDir, sub)); err != nil {
			return true
		}
	}
	return false
}

// slidevViteConfigTS is merged by Slidev with its internal Vite config (see resolveViteConfigs).
// Without server.allowedHosts, tunnel hostnames (e.g. *.bjb1.seetacloud.com) get HTTP 403 on preview ports like 6008.
const slidevViteConfigTS = `import { defineConfig } from 'vite'

export default defineConfig({
  server: {
    allowedHosts: true,
  },
  preview: {
    allowedHosts: true,
  },
})
`

func trimDepsLog(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// ensureSlidevSharedNodeModules runs npm install once under slidevSharedDepsDir when cache is missing or empty.
func (s *PPTService) ensureSlidevSharedNodeModules() {
	_ = os.MkdirAll(slidevSharedDepsDir, 0755)
	if err := os.WriteFile(filepath.Join(slidevSharedDepsDir, "package.json"), []byte(slidevPackageJSON), 0644); err != nil {
		s.state.BroadcastPPTLog("[deps] shared cache: failed to write package.json: " + err.Error())
		return
	}
	nm := filepath.Join(slidevSharedDepsDir, "node_modules")
	entries, errDir := os.ReadDir(nm)
	if errDir == nil && len(entries) > 0 && !slidevSharedCacheIncomplete(nm) {
		return
	}
	if errDir == nil && len(entries) > 0 {
		s.state.BroadcastPPTLog("[deps] shared cache: incomplete (new theme deps?), running npm install in " + slidevSharedDepsDir)
	}
	ctx, cancel := context.WithTimeout(context.Background(), slidevNPMInstallTimeout)
	defer cancel()
	s.state.BroadcastPPTLog("[deps] shared cache: npm install (network) in " + slidevSharedDepsDir)
	stdout, stderr, err := tools.ExecuteCommand(ctx, "npm install", slidevSharedDepsDir)
	if err != nil {
		msg := trimDepsLog(stderr, 800)
		if msg == "" {
			msg = trimDepsLog(stdout, 800)
		}
		s.state.BroadcastPPTLog(fmt.Sprintf("[deps] shared cache npm failed: %v %s", err, msg))
		return
	}
	if t := trimDepsLog(stdout, 400); t != "" {
		s.state.BroadcastPPTLog("[deps] shared cache npm output: " + t)
	}
	s.state.BroadcastPPTLog("[deps] shared cache ready")
}

// linkSlidevNodeModules symlinks workDir/node_modules -> shared cache (relative link).
func (s *PPTService) linkSlidevNodeModules(workDir string) error {
	sharedNM := filepath.Join(slidevSharedDepsDir, "node_modules")
	st, err := os.Stat(sharedNM)
	if err != nil {
		return fmt.Errorf("shared node_modules not ready: %w", err)
	}
	if !st.IsDir() {
		return fmt.Errorf("shared node_modules is not a directory")
	}
	target := filepath.Join(workDir, "node_modules")
	_ = os.RemoveAll(target)
	rel, err := filepath.Rel(workDir, sharedNM)
	if err != nil {
		return err
	}
	if err := os.Symlink(rel, target); err != nil {
		return err
	}
	return nil
}

func (s *PPTService) npmInstallInDir(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), slidevNPMInstallTimeout)
	defer cancel()
	stdout, stderr, err := tools.ExecuteCommand(ctx, "npm install", dir)
	if err != nil {
		msg := trimDepsLog(stderr, 800)
		if msg == "" {
			msg = trimDepsLog(stdout, 800)
		}
		return fmt.Errorf("%w %s", err, msg)
	}
	if t := trimDepsLog(stdout, 400); t != "" {
		s.state.BroadcastPPTLog("[deps] npm install output: " + t)
	}
	return nil
}

func (s *PPTService) ensureSlidevViteConfig(workDir string) {
	if strings.TrimSpace(workDir) == "" {
		return
	}
	path := filepath.Join(workDir, "vite.config.ts")
	if err := os.WriteFile(path, []byte(slidevViteConfigTS), 0644); err != nil {
		s.state.BroadcastPPTLog("[deps] vite.config.ts: " + err.Error())
	}
}

// installSlidevNPM links each project to a shared node_modules copy; network only when filling the shared cache.
func (s *PPTService) installSlidevNPM(workDir string) {
	if strings.TrimSpace(workDir) == "" {
		return
	}
	defer s.ensureSlidevViteConfig(workDir)
	s.state.BroadcastPPTLog("[deps] prepare node_modules for " + workDir)
	s.ensureSlidevSharedNodeModules()
	if err := s.linkSlidevNodeModules(workDir); err != nil {
		s.state.BroadcastPPTLog("[deps] symlink failed (" + err.Error() + "), falling back to npm install in project")
		if err := s.npmInstallInDir(workDir); err != nil {
			s.state.BroadcastPPTLog("[deps] npm install in project failed: " + err.Error())
			return
		}
		s.state.BroadcastPPTLog("[deps] npm install in project ok")
		return
	}
	s.state.BroadcastPPTLog("[deps] linked node_modules to shared cache")
}

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
		llmCfg := toolcalling.LLMConfig{
			APIKey:  os.Getenv("OPENAI_API_KEY"),
			Model:   os.Getenv("OPENAI_MODEL"),
			BaseURL: os.Getenv("OPENAI_BASE_URL"),
		}
		// MiniMax: reasoning_split can return HTTP 529 on some clusters (e.g. api.minimaxi.com). Enable only when needed.
		if strings.EqualFold(strings.TrimSpace(os.Getenv("OPENAI_REASONING_SPLIT")), "true") {
			llmCfg.ExtraBody = map[string]any{"reasoning_split": true}
		}
		svc.agent = toolcalling.NewAgent(llmCfg)
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

// pathUnderBase reports whether p is base or a path inside base (after Clean).
func pathUnderBase(base, p string) bool {
	base = filepath.Clean(base)
	p = filepath.Clean(p)
	if p == base {
		return true
	}
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// mustResolveProjectWritePath maps a relative or absolute path to an absolute path that must lie under the PPT workdir.
func (s *PPTService) mustResolveProjectWritePath(relOrAbs string) (string, error) {
	wd := strings.TrimSpace(s.state.GetPPTWorkDir())
	if wd == "" {
		return "", fmt.Errorf("ppt project directory is not set")
	}
	wd = filepath.Clean(wd)
	var full string
	if filepath.IsAbs(relOrAbs) {
		full = filepath.Clean(relOrAbs)
	} else {
		full = filepath.Clean(filepath.Join(wd, relOrAbs))
	}
	if !pathUnderBase(wd, full) {
		return "", fmt.Errorf("path must stay inside the PPT project directory %s (got %q)", wd, relOrAbs)
	}
	return full, nil
}

// registerTools wires all PPT agent tools into the underlying agent.
func (s *PPTService) registerTools() {
	resolvePath := func(path string) string {
		if len(path) > 0 && path[0] == '/' {
			return path
		}
		return s.state.GetPPTWorkDir() + "/" + path
	}
	// resolveWorkdir makes execute_command match file tools: "." and relative paths
	// are under the PPT project dir, not the Go server's process cwd.
	resolveWorkdir := func(workdir string) string {
		w := strings.TrimSpace(workdir)
		if w == "" || w == "." || w == "./" {
			return s.state.GetPPTWorkDir()
		}
		if filepath.IsAbs(w) {
			return filepath.Clean(w)
		}
		return filepath.Join(s.state.GetPPTWorkDir(), w)
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
		Description: "Drain all pending user/voice messages from the queue in one call (oldest first, joined with ' | '). If empty, returns queue is empty.",
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
			full, err := s.mustResolveProjectWritePath(path)
			if err != nil {
				return "", err
			}
			if err := tools.EditFile(ctx, full, oldStr, newStr); err != nil {
				return "", err
			}
			return "file edited successfully", nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name: "write_file",
		Description: "Overwrite the entire file. For slides.md, use this only for a short bootstrap (e.g. headmatter + first slide) or small full rewrites; " +
			"prefer append_file to add slides one by one.",
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
			full, err := s.mustResolveProjectWritePath(path)
			if err != nil {
				return "", err
			}
			if err := tools.WriteFile(ctx, full, content); err != nil {
				return "", err
			}
			return "file written successfully", nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name: "append_file",
		Description: "Append content to a file (create if it does not exist). For slides.md, append one slide per call: each new slide should normally start with the Slidev separator line --- " +
			"(and a blank line) before the slide body, except when bootstrapping the file with write_file first.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string"},
				"content": map[string]any{"type": "string"},
			},
			"required": []any{"path", "content"},
		},
		Function: s.logTool("append_file", func(ctx context.Context, args map[string]any) (string, error) {
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			full, err := s.mustResolveProjectWritePath(path)
			if err != nil {
				return "", err
			}
			if err := tools.AppendFile(ctx, full, content); err != nil {
				return "", err
			}
			return "content appended successfully", nil
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
			srcFull, err := s.mustResolveProjectWritePath(src)
			if err != nil {
				return "", err
			}
			dstFull, err := s.mustResolveProjectWritePath(dst)
			if err != nil {
				return "", err
			}
			if err := tools.MoveFile(ctx, srcFull, dstFull); err != nil {
				return "", err
			}
			return "file moved successfully", nil
		}),
	})

	s.agent.AddTool(toolcalling.Tool{
		Name: "execute_command",
		Description: "Execute a shell command. workdir is optional: omit it, use \".\", or use a path relative to the PPT project directory (same root as write_file slides). " +
			"Never run `slidev`/`vite`/`npm run dev` in the foreground — they block until killed. Use `nohup ... > /tmp/some.log 2>&1 &` then `sleep 1` and `tail` the log if you need output. " +
			"Only use an absolute path if you intentionally need another directory.",
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
			fullWd := resolveWorkdir(workdir)
			wdRoot := filepath.Clean(s.state.GetPPTWorkDir())
			if wdRoot == "" {
				return "", fmt.Errorf("ppt project directory is not set")
			}
			if !pathUnderBase(wdRoot, filepath.Clean(fullWd)) {
				return "", fmt.Errorf("execute_command workdir must be inside project %s", wdRoot)
			}
			stdout, stderr, err := tools.ExecuteCommand(ctx, cmd, fullWd)
			if err != nil {
				if stderr != "" {
					return "", fmt.Errorf("%w\nstderr: %s", err, stderr)
				}
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
	msgs := s.state.DrainVoiceMessageQueue()
	if len(msgs) == 0 {
		return "queue is empty", nil
	}
	return strings.Join(msgs, " | "), nil
}

func (s *PPTService) buildSystemMessage() openai.ChatCompletionMessageParamUnion {
	queueLen := s.state.VoiceMessageQueueLen()
	queueStatus := "empty"
	if queueLen > 0 {
		queueStatus = fmt.Sprintf("has %d pending message(s)", queueLen)
	}
	workDir := s.state.GetPPTWorkDir()
	if workDir == "" {
		workDir = "(unset)"
	}
	content := fmt.Sprintf(
		"You are a PPT generation agent. Use the available tools to create the presentation. "+
			"CRITICAL: The ONLY project directory for this job is `%s` (it is already created from the user's topic). "+
			"All slides.md, package.json, exports, and shell commands must live there: use relative paths like `slides.md` and execute_command with workdir `.` or omit workdir. "+
			"Do NOT mkdir or write files under a different path such as `/root/autodl-tmp/workspace/slidev-*` or any sibling folder — write/append/move tools will reject paths outside the project directory. "+
			"You may still read_skill documentation with read_file using absolute paths under `/root/autodl-tmp/workspace/skills/`. "+
			"When building slides.md, prefer small steps: use write_file once for a minimal bootstrap (headmatter + first slide), then use append_file repeatedly to add ONE slide per tool call (each new slide starts with a --- separator line as in Slidev). "+
			"Avoid a single enormous write_file for the whole deck unless you are replacing a short file. "+
			"Before writing the first slides.md, read `/root/autodl-tmp/workspace/skills/slidev/SKILL.md` and `/root/autodl-tmp/workspace/skills/slidev/references/core-syntax.md`. "+
			"SKILL.md is the index into `/root/autodl-tmp/workspace/skills/slidev/references/`; explore that directory and any linked files as needed—there is no need to limit yourself to a minimal subset. "+
			"For how reference files are grouped and named, see `/root/autodl-tmp/workspace/skills/GENERATION.md`. "+
			"IMPORTANT: The deck MUST include at least one click-driven or step-through interaction (not static-only); choose approaches by reading the relevant references you discover via SKILL.md. "+
			"MANDATORY Slidev viewport rules (prevents bottom content being clipped in browser/PDF): "+
			"(1) Each slide MUST fit one 16:9 viewport without relying on the viewer to guess: at most ONE fenced code block per slide; never place two topics with full examples on the same slide (e.g. List + Dictionary → two slides). "+
			"(2) Do NOT stack multiple large code blocks in one `<v-clicks>` on a single slide; split into consecutive slides so each slide stays short. "+
			"(3) Keep each code block to about 14 lines or fewer; shorten examples or continue on the next slide. "+
			"(4) Do not combine a long bullet list and a tall code block on the same slide. "+
			"(5) In the deck headmatter, add `style:` with CSS such as `.slidev-page { overflow-y: auto; padding-bottom: 2.5rem; }` as a safety net (adjust selector if needed per theme). "+
			"CODE RUNNERS: This deployment does NOT register a Python runner for Slidev. NEVER use `{monaco-run}` on ```python blocks — it shows errors like \"Runner for language \\\"python\\\" not found\". "+
			"Use ordinary ```python (Shiki highlighting only) for all Python; interactivity must use v-click / v-clicks / motion / layouts, not Python monaco-run. "+
			"Optional: at most one short ```ts {monaco-run} or ```js {monaco-run} snippet in the whole deck if a runnable demo is essential. "+
			"Theme choice: dependencies include several Slidev themes — pick exactly ONE via headmatter `theme:` based on topic and audience. "+
			"Allowed values (short id → vibe): `default` (neutral, safe), `seriph` (elegant serif), `bricks` (bold blocks), `shibainu` (friendly / playful), `apple-basic` (clean minimal). "+
			"Do NOT use any other theme name or community package not preinstalled. "+
			"IMPORTANT: If the content contains Chinese characters, add the following to the frontmatter to avoid garbled text in PDF: "+
			"`fonts:\\n  sans: 'Noto Sans SC'\\n  serif: 'Noto Serif SC'\\n  mono: 'Fira Code'`. "+
			"Dependencies are usually provided via a shared `node_modules` symlink when the session starts (see PPT log [deps]); if export fails, run `npm install` in the workdir or fix the shared cache under `/root/autodl-tmp/workspace/.slidev-shared/`. "+
			"Also run `apt-get install -y libgbm1 libasound2 2>/dev/null || true` to ensure system libraries are available. "+
			"Then run `npx slidev export slides.md --output ppt.pdf` to produce the final PDF. "+
			"If the user asks to open or expose the deck on a TCP port (e.g. 6008 for browser preview), start Slidev in the background from the project dir. "+
			"The Slidev CLI bundled with this project does NOT support `--host` (you get \"Unknown argument: host\"). To listen on all interfaces you MUST pass `--remote <password>` (any non-empty passphrase); that enables bind on 0.0.0.0 (`--bind` defaults to 0.0.0.0). "+
			"Use exactly: `nohup npx slidev slides.md --port 6008 --remote slidev-preview --log warn > /tmp/slidev-6008.log 2>&1 & sleep 3; tail -40 /tmp/slidev-6008.log` — then read the log lines that show LAN/public URLs and tell the user `http://<their-host>:6008/?password=slidev-preview` (or the `remote control` URL from the log, with tunnel hostname substituted for localhost). "+
			"Never run `slidev` in the foreground; never use only trailing `&` without `nohup` for preview (the session can hang or die on SIGHUP). "+
			"The workdir includes vite.config.ts (allowedHosts for cloud URLs); if preview returns 403 Blocked request for the tunnel host, stop the old slidev process and start it again so Vite picks up that file. "+
			"After the PDF is successfully exported (and any preview server requested is started), you MUST call send_to_voice_agent with a concise status for the voice agent, including preview URL if applicable. "+
			"When stuck on Slidev behavior, return to SKILL.md and follow links into references/. "+
			"Note: when there are no pending voice messages, your runtime pauses after you finish — the user must speak again to enqueue more work. "+
			"Current voice message queue status: %s. "+
			"If the queue has messages, call fetch_from_voice_message_queue once to consume **all** pending items (joined with ' | ').",
		workDir,
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

	// Pre-seed a valid package.json so npm never needs to guess the package name
	// from the (possibly Chinese) directory name, which npm rejects.
	_ = os.WriteFile(filepath.Join(workDir, "package.json"), []byte(slidevPackageJSON), 0644)

	s.installSlidevNPM(workDir)

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
	wd := s.state.GetPPTWorkDir()
	if wd != "" {
		s.ensureSlidevViteConfig(wd)
		if _, err := os.Stat(filepath.Join(wd, "node_modules")); err != nil && os.IsNotExist(err) {
			s.installSlidevNPM(wd)
		}
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

			history = s.compressPPTHistoryIfNeeded(ctx, history)

			s.runChatMu.RLock()
			fn := s.runChatFn
			s.runChatMu.RUnlock()

			msgs, err := fn(ctx, history)
			if err != nil {
				s.state.BroadcastPPTLog("[error] ppt agent chat: " + err.Error())
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
	s.ReleaseSlidevPreviewPort()
	s.runtime.Stop()
}

// WaitRuntime blocks until the PPT agent runtime goroutine exits.
func (s *PPTService) WaitRuntime() {
	s.runtime.Wait()
}
