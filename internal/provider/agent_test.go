package provider

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// run feeds lines through a translator and returns the chunk types, with
// every chunk kept for closer checks.
func run(t *testing.T, tr translator, lines ...string) ([]string, []Chunk) {
	t.Helper()
	var chunks []Chunk
	for _, line := range lines {
		if err := tr.handle([]byte(line), func(c Chunk) error { chunks = append(chunks, c); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	types := make([]string, len(chunks))
	for i, c := range chunks {
		types[i] = c["type"].(string)
	}
	return types, chunks
}

func TestClaudeStreamBecomesUIChunks(t *testing.T) {
	types, chunks := run(t, &claudeTranslator{},
		`{"type":"system","subtype":"init","session_id":"s1"}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"id":"m1"}},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"thinking"}},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"t1","name":"Bash"}},"parent_tool_use_id":null}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"trellis card ls"}}]},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1},"parent_tool_use_id":null}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"TEST-1"}]}]},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"sub"}},"parent_tool_use_id":"t9"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"text"}},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Done"}},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2},"parent_tool_use_id":null}`,
		`{"type":"result","subtype":"success","is_error":false}`,
	)
	want := []string{"message-metadata", "reasoning-start", "reasoning-delta", "reasoning-end",
		"tool-input-start", "tool-input-available", "tool-output-available",
		"text-start", "text-delta", "text-end"}
	if !slices.Equal(types, want) {
		t.Fatalf("types = %v\nwant    %v", types, want)
	}
	if chunks[6]["output"] != "TEST-1" || chunks[8]["delta"] != "Done" || chunks[8]["id"] != "m1-2" {
		t.Fatalf("chunks = %v", chunks)
	}
}

func TestClaudeErrorResultIsReported(t *testing.T) {
	tr := &claudeTranslator{}
	types, _ := run(t, tr, `{"type":"result","subtype":"error_during_execution","is_error":true,"result":"boom"}`)
	if !slices.Equal(types, []string{"error"}) || !tr.failed() {
		t.Fatalf("types = %v, failed = %v", types, tr.failed())
	}
}

func TestCodexItemsBecomeUIChunks(t *testing.T) {
	types, chunks := run(t, &codexTranslator{},
		`{"type":"thread.started","thread_id":"th1"}`,
		`{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":""}}`,
		`{"type":"item.started","item":{"id":"i1","type":"command_execution","command":"trellis card ls","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"trellis card ls","aggregated_output":"TEST-1\n","exit_code":0,"status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"i2","type":"command_execution","command":"false","aggregated_output":"","exit_code":1,"status":"failed"}}`,
		`{"type":"item.completed","item":{"id":"i3","type":"agent_message","text":"Done"}}`,
		`{"type":"turn.completed"}`,
	)
	want := []string{"message-metadata", "tool-input-available", "tool-output-available",
		"tool-input-available", "tool-output-error", "text-start", "text-delta", "text-end"}
	if !slices.Equal(types, want) {
		t.Fatalf("types = %v\nwant    %v", types, want)
	}
	if meta := chunks[0]["messageMetadata"].(map[string]any); meta["session"] != "th1" {
		t.Fatalf("metadata = %v", meta)
	}
}

func TestCommandScopesTheAgentToTheProject(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		for _, session := range []string{"", "s1"} {
			turn := Turn{Project: "TEST", Prompt: "hi", Session: session, Root: "/tmp/root", Actor: "agent:chat-x"}
			turn.Provider.ID, turn.Provider.Kind, turn.Provider.Dir = "x", kind, "/tmp"
			cmd, err := command(t.Context(), turn)
			if err != nil {
				t.Fatal(err)
			}
			env := strings.Join(cmd.Env, "\n")
			for _, want := range []string{"TRELLIS_PROJECT=TEST", "TRELLIS_AGENT=agent:chat-x", "TRELLIS_HOME=/tmp/root"} {
				if !strings.Contains(env, want) {
					t.Errorf("%s: env lacks %s", kind, want)
				}
			}
			args := strings.Join(cmd.Args, " ")
			if session != "" && !strings.Contains(args, session) {
				t.Errorf("%s: args %q do not resume %s", kind, args, session)
			}
			if cmd.Dir != "/tmp" {
				t.Errorf("%s: dir = %q", kind, cmd.Dir)
			}
		}
	}
}

func TestCommandExpandsHomeInTheDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	turn := Turn{Project: "TEST", Prompt: "hi", Root: "/tmp/root"}
	turn.Provider.ID, turn.Provider.Kind, turn.Provider.Dir = "x", "claude-code", "~/code"
	cmd, err := command(t.Context(), turn)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != filepath.Join(home, "code") {
		t.Fatalf("dir = %q", cmd.Dir)
	}
}

func TestAPromptNeverBecomesAnArgument(t *testing.T) {
	for _, kind := range []string{"claude-code", "codex"} {
		turn := Turn{Project: "TEST", Prompt: "--dangerously-bypass-approvals-and-sandbox", Root: "/tmp/root"}
		turn.Provider.ID, turn.Provider.Kind = "x", kind
		cmd, err := command(t.Context(), turn)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(cmd.Args, turn.Prompt) {
			t.Errorf("%s: prompt passed as an argument: %q", kind, cmd.Args)
		}
		in, _ := io.ReadAll(cmd.Stdin)
		if !strings.Contains(string(in), turn.Prompt) {
			t.Errorf("%s: stdin = %q", kind, in)
		}
	}
}

func TestASessionThatIsNotAnIDIsRefused(t *testing.T) {
	for _, session := range []string{"--last", "-x", "a b", "a;b"} {
		turn := Turn{Project: "TEST", Prompt: "hi", Session: session, Root: "/tmp/root"}
		turn.Provider.ID, turn.Provider.Kind = "x", "codex"
		if _, err := command(t.Context(), turn); err == nil {
			t.Errorf("session %q accepted", session)
		}
	}
}
