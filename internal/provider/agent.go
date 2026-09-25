package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mtch3n/trellis/internal/config"
)

// Chunk is one part of an AI SDK UI message stream: the JSON object the
// browser's useChat reads from each server-sent event.
type Chunk map[string]any

// Turn is one message sent to a local agent from the web UI's chat.
type Turn struct {
	Provider config.Provider
	// Project is the key the chat is scoped to. The agent's trellis commands
	// resolve to it through TRELLIS_PROJECT.
	Project string
	Prompt  string
	// Session is the agent's own id for the conversation, from the previous
	// turn's metadata. Empty starts a new one.
	Session string
	// Root is the storage root the agent's trellis commands must use and,
	// under a sandbox, be allowed to write.
	Root string
	// Actor is who the agent's writes are recorded as.
	Actor string
}

// instructions tell the agent where it is. Claude Code takes them as an
// appended system prompt; Codex, which has no such flag, reads them before
// the first message of a conversation.
func instructions(t Turn) string {
	return fmt.Sprintf(`You are the assistant inside the Trellis web dashboard, talking with the person who owns project %[1]s.
Use the trellis CLI to read and change the project's cards and vault. TRELLIS_PROJECT and TRELLIS_AGENT are already set: run plain "trellis ..." commands, never with an environment prefix.
Useful commands: trellis card ls, trellis card show <ref>, trellis card new, trellis card comment, trellis card move, trellis vault ls, trellis vault show <entry>, trellis search <query>.
Answer in concise Markdown. Cite cards by ref (%[1]s-12) and entries by slug.`, t.Project)
}

// sessionID is the shape both agents give their conversations. A session
// comes back from the browser, and it is a command-line argument, so nothing
// else may pass: in particular nothing that starts with a dash.
var sessionID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// command builds the process for one turn. The prompt is the person's text,
// so it goes in on stdin, never as an argument an agent could read as a flag.
func command(ctx context.Context, t Turn) (*exec.Cmd, error) {
	if t.Session != "" && !sessionID.MatchString(t.Session) {
		return nil, fmt.Errorf("session %q is not an agent session id", t.Session)
	}
	p := t.Provider
	prompt := t.Prompt
	var args []string
	switch p.Kind {
	case "claude-code":
		args = []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages",
			"--append-system-prompt", instructions(t),
			// The Trellis plugin's hooks give every session an identity of
			// its own, which would replace Actor. The instructions above say
			// what its brief would, so the chat runs without hooks, with only
			// the tools it needs, and may run trellis and read without asking.
			"--settings", `{"disableAllHooks":true}`,
			"--tools", "Bash,Read,Grep,Glob",
			"--allowedTools", "Bash(trellis *)", "Read", "Grep", "Glob"}
		if t.Session != "" {
			args = append(args, "--resume", t.Session)
		}
		if p.Model != "" {
			args = append(args, "--model", p.Model)
		}
		if p.Effort != "" {
			args = append(args, "--effort", p.Effort)
		}
	case "codex":
		// resume takes no --sandbox or --cd, so the sandbox is set through
		// config for both, and the directory through the process.
		sandbox := []string{"--json", "--skip-git-repo-check",
			"-c", `sandbox_mode="workspace-write"`,
			"-c", fmt.Sprintf("sandbox_workspace_write.writable_roots=[%q]", t.Root)}
		if p.Model != "" {
			sandbox = append(sandbox, "--model", p.Model)
		}
		if p.Effort != "" {
			sandbox = append(sandbox, "-c", fmt.Sprintf("model_reasoning_effort=%q", p.Effort))
		}
		if t.Session != "" {
			args = append([]string{"exec", "resume"}, sandbox...)
			args = append(args, t.Session, "-")
		} else {
			args = append([]string{"exec"}, sandbox...)
			args = append(args, "-")
			prompt = instructions(t) + "\n\n" + t.Prompt
		}
	default:
		return nil, fmt.Errorf("provider %s is not a local agent", p.ID)
	}
	cmd := exec.CommandContext(ctx, p.Executable(), args...)
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	// Dir is written by a person, so ~ means their home, as in a shell.
	switch {
	case p.Dir == "" || p.Dir == "~":
		cmd.Dir = home
	case strings.HasPrefix(p.Dir, "~/"):
		cmd.Dir = filepath.Join(home, p.Dir[2:])
	default:
		cmd.Dir = p.Dir
	}
	cmd.Env = append(os.Environ(),
		"TRELLIS_PROJECT="+t.Project,
		"TRELLIS_AGENT="+t.Actor,
		"TRELLIS_HOME="+t.Root,
	)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.WaitDelay = 5 * time.Second
	return cmd, nil
}

// Stream runs one turn of a local agent and sends what it does to emit as UI
// message chunks, ending with a finish chunk. Cancelling ctx ends the
// process: the browser closing the stream stops the agent.
func Stream(ctx context.Context, t Turn, emit func(Chunk) error) error {
	cmd, err := command(ctx, t)
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, n: 8 << 10}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", t.Provider.Executable(), err)
	}
	if err := emit(Chunk{"type": "start", "messageMetadata": map[string]any{"provider": t.Provider.ID}}); err != nil {
		_ = cmd.Cancel()
		_ = cmd.Wait()
		return err
	}
	var tr translator
	if t.Provider.Kind == "codex" {
		tr = &codexTranslator{}
	} else {
		tr = &claudeTranslator{}
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	var emitErr error
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if emitErr = tr.handle(line, emit); emitErr != nil {
			break
		}
	}
	scanErr := scanner.Err()
	if emitErr != nil || scanErr != nil {
		_ = cmd.Cancel()
	}
	waitErr := cmd.Wait()
	if emitErr == nil && scanErr != nil {
		emitErr = emit(Chunk{"type": "error", "errorText": "reading the agent's output: " + scanErr.Error()})
		if emitErr == nil {
			return emit(Chunk{"type": "finish"})
		}
	}
	if emitErr != nil {
		return emitErr
	}
	if waitErr != nil && ctx.Err() == nil && !tr.failed() {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		if err := emit(Chunk{"type": "error", "errorText": msg}); err != nil {
			return err
		}
	}
	return emit(Chunk{"type": "finish"})
}

// translator turns one agent's JSON event lines into UI message chunks.
type translator interface {
	handle(line []byte, emit func(Chunk) error) error
	// failed reports whether the agent already said why it stopped, so a
	// non-zero exit needs no second error.
	failed() bool
}

// text emits a whole text or reasoning part at once, for an agent that only
// reports finished items.
func text(emit func(Chunk) error, kind, id, body string) error {
	if body == "" {
		return nil
	}
	for _, c := range []Chunk{
		{"type": kind + "-start", "id": id},
		{"type": kind + "-delta", "id": id, "delta": body},
		{"type": kind + "-end", "id": id},
	} {
		if err := emit(c); err != nil {
			return err
		}
	}
	return nil
}

type claudeBlock struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Input     jsontext.Value `json:"input"`
	ToolUseID string         `json:"tool_use_id"`
	Content   jsontext.Value `json:"content"`
	IsError   bool           `json:"is_error"`
}

type claudeEvent struct {
	Type      string  `json:"type"`
	Subtype   string  `json:"subtype"`
	SessionID string  `json:"session_id"`
	Parent    *string `json:"parent_tool_use_id"`
	IsError   bool    `json:"is_error"`
	Result    string  `json:"result"`
	Event     *struct {
		Type    string `json:"type"`
		Index   int    `json:"index"`
		Message *struct {
			ID string `json:"id"`
		} `json:"message"`
		ContentBlock *claudeBlock `json:"content_block"`
		Delta        *struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	} `json:"event"`
	Message *struct {
		Content []claudeBlock `json:"content"`
	} `json:"message"`
}

// claudeTranslator reads `claude -p --output-format stream-json
// --include-partial-messages`: text and thinking arrive as deltas, a tool
// call's input whole in the assistant message, and its result in the next
// user message.
type claudeTranslator struct {
	message string
	open    map[int]string // content block index -> "text" or "reasoning"
	started map[string]bool
	errored bool
}

func (c *claudeTranslator) failed() bool { return c.errored }

func (c *claudeTranslator) blockID(index int) string { return fmt.Sprintf("%s-%d", c.message, index) }

func (c *claudeTranslator) handle(line []byte, emit func(Chunk) error) error {
	var ev claudeEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return nil
	}
	// A subagent's own stream is its business; its result reaches the
	// conversation as the tool result of the call that started it.
	if ev.Parent != nil {
		return nil
	}
	if c.open == nil {
		c.open = map[int]string{}
		c.started = map[string]bool{}
	}
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" && ev.SessionID != "" {
			return emit(Chunk{"type": "message-metadata", "messageMetadata": map[string]any{"session": ev.SessionID}})
		}
	case "stream_event":
		e := ev.Event
		if e == nil {
			return nil
		}
		switch e.Type {
		case "message_start":
			if e.Message != nil {
				c.message = e.Message.ID
			}
		case "content_block_start":
			if e.ContentBlock == nil {
				return nil
			}
			switch e.ContentBlock.Type {
			case "text":
				c.open[e.Index] = "text"
			case "thinking":
				c.open[e.Index] = "reasoning"
			case "tool_use":
				c.started[e.ContentBlock.ID] = true
				return emit(Chunk{"type": "tool-input-start", "toolCallId": e.ContentBlock.ID,
					"toolName": e.ContentBlock.Name, "dynamic": true})
			default:
				return nil
			}
			return emit(Chunk{"type": c.open[e.Index] + "-start", "id": c.blockID(e.Index)})
		case "content_block_delta":
			kind, ok := c.open[e.Index]
			if !ok || e.Delta == nil {
				return nil
			}
			delta := e.Delta.Text
			if kind == "reasoning" {
				delta = e.Delta.Thinking
			}
			if delta == "" {
				return nil
			}
			return emit(Chunk{"type": kind + "-delta", "id": c.blockID(e.Index), "delta": delta})
		case "content_block_stop":
			kind, ok := c.open[e.Index]
			if !ok {
				return nil
			}
			delete(c.open, e.Index)
			return emit(Chunk{"type": kind + "-end", "id": c.blockID(e.Index)})
		}
	case "assistant":
		if ev.Message == nil {
			return nil
		}
		for _, b := range ev.Message.Content {
			if b.Type != "tool_use" {
				continue
			}
			if !c.started[b.ID] {
				if err := emit(Chunk{"type": "tool-input-start", "toolCallId": b.ID, "toolName": b.Name, "dynamic": true}); err != nil {
					return err
				}
			}
			var input any = map[string]any{}
			if len(b.Input) > 0 {
				_ = json.Unmarshal(b.Input, &input)
			}
			if err := emit(Chunk{"type": "tool-input-available", "toolCallId": b.ID, "toolName": b.Name,
				"input": input, "dynamic": true}); err != nil {
				return err
			}
		}
	case "user":
		if ev.Message == nil {
			return nil
		}
		for _, b := range ev.Message.Content {
			if b.Type != "tool_result" || b.ToolUseID == "" {
				continue
			}
			out := toolResultText(b.Content)
			chunk := Chunk{"type": "tool-output-available", "toolCallId": b.ToolUseID, "output": out, "dynamic": true}
			if b.IsError {
				chunk = Chunk{"type": "tool-output-error", "toolCallId": b.ToolUseID, "errorText": out, "dynamic": true}
			}
			if err := emit(chunk); err != nil {
				return err
			}
		}
	case "result":
		if ev.IsError {
			c.errored = true
			msg := ev.Result
			if msg == "" {
				msg = "The agent stopped with an error (" + ev.Subtype + ")."
			}
			return emit(Chunk{"type": "error", "errorText": msg})
		}
	}
	return nil
}

// toolResultText reads a tool result's content, which is either a string or
// a list of content blocks.
func toolResultText(raw jsontext.Value) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

type codexItem struct {
	ID               string         `json:"id"`
	Type             string         `json:"type"`
	Text             string         `json:"text"`
	Command          string         `json:"command"`
	AggregatedOutput string         `json:"aggregated_output"`
	ExitCode         *int           `json:"exit_code"`
	Status           string         `json:"status"`
	Server           string         `json:"server"`
	Tool             string         `json:"tool"`
	Arguments        jsontext.Value `json:"arguments"`
	Result           jsontext.Value `json:"result"`
	Changes          jsontext.Value `json:"changes"`
	Message          string         `json:"message"`
}

type codexEvent struct {
	Type     string     `json:"type"`
	ThreadID string     `json:"thread_id"`
	Item     *codexItem `json:"item"`
	Message  string     `json:"message"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// codexTranslator reads `codex exec --json`, which reports whole items as
// they start and complete rather than token deltas.
type codexTranslator struct {
	started map[string]bool
	errored bool
}

func (c *codexTranslator) failed() bool { return c.errored }

// codexTool names a tool item and its input, or reports it is not one.
func codexTool(it *codexItem) (string, any, bool) {
	switch it.Type {
	case "command_execution":
		return "shell", map[string]any{"command": it.Command}, true
	case "mcp_tool_call":
		var args any
		_ = json.Unmarshal(it.Arguments, &args)
		return it.Server + "." + it.Tool, args, true
	case "file_change":
		var changes any
		_ = json.Unmarshal(it.Changes, &changes)
		return "edit", map[string]any{"changes": changes}, true
	case "web_search":
		return "web_search", map[string]any{"query": it.Text}, true
	}
	return "", nil, false
}

func (c *codexTranslator) handle(line []byte, emit func(Chunk) error) error {
	var ev codexEvent
	if err := json.Unmarshal(line, &ev); err != nil {
		return nil
	}
	if c.started == nil {
		c.started = map[string]bool{}
	}
	switch ev.Type {
	case "thread.started":
		return emit(Chunk{"type": "message-metadata", "messageMetadata": map[string]any{"session": ev.ThreadID}})
	case "item.started", "item.completed":
		it := ev.Item
		if it == nil {
			return nil
		}
		if name, input, ok := codexTool(it); ok {
			if !c.started[it.ID] {
				c.started[it.ID] = true
				if err := emit(Chunk{"type": "tool-input-available", "toolCallId": it.ID, "toolName": name,
					"input": input, "dynamic": true}); err != nil {
					return err
				}
			}
			if ev.Type != "item.completed" {
				return nil
			}
			if it.Status == "failed" || (it.ExitCode != nil && *it.ExitCode != 0) {
				msg := strings.TrimSpace(it.AggregatedOutput)
				if msg == "" {
					msg = "failed"
				}
				return emit(Chunk{"type": "tool-output-error", "toolCallId": it.ID, "errorText": msg, "dynamic": true})
			}
			var output any = it.AggregatedOutput
			if it.Type == "mcp_tool_call" {
				_ = json.Unmarshal(it.Result, &output)
			}
			if it.Type == "file_change" {
				output = it.Status
			}
			return emit(Chunk{"type": "tool-output-available", "toolCallId": it.ID, "output": output, "dynamic": true})
		}
		if ev.Type != "item.completed" {
			return nil
		}
		switch it.Type {
		case "agent_message":
			return text(emit, "text", it.ID, it.Text)
		case "reasoning":
			return text(emit, "reasoning", it.ID, it.Text)
		case "error":
			return emit(Chunk{"type": "error", "errorText": it.Message})
		}
	case "turn.failed":
		c.errored = true
		msg := "The agent's turn failed."
		if ev.Error != nil && ev.Error.Message != "" {
			msg = ev.Error.Message
		}
		return emit(Chunk{"type": "error", "errorText": msg})
	case "error":
		c.errored = true
		return emit(Chunk{"type": "error", "errorText": ev.Message})
	}
	return nil
}

// limitedWriter keeps the first n bytes written to it and drops the rest, so
// a chatty agent's stderr cannot grow without bound.
type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		keep := p[:min(len(p), l.n)]
		if _, err := l.w.Write(keep); err != nil {
			return 0, err
		}
		l.n -= len(keep)
	}
	return len(p), nil
}

// IsLocal reports whether p is run by the daemon as a program.
func IsLocal(p config.Provider) bool {
	kind, ok := config.LookupProviderKind(p.Kind)
	return ok && kind.Local
}
