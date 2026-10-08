package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeEvent(t *testing.T) {
	ev := Event{
		HookEventName:  "preToolUse",
		ConversationID: "cursor-conversation",
		ToolName:       "Write",
	}
	normalizeEvent(hostCursor, &ev)
	if ev.host != hostCursor || ev.HookEventName != "PreToolUse" ||
		ev.SessionID != "cursor-conversation" {
		t.Fatalf("unexpected normalized event: %+v", ev)
	}
	if !ev.isGatedTool() {
		t.Fatal("Cursor Write should be gated")
	}

	codex := Event{host: hostCodex, ToolName: "apply_patch"}
	if !codex.isGatedTool() {
		t.Fatal("Codex apply_patch should be gated")
	}
}

func TestEventMatchesHost(t *testing.T) {
	cursorEvent := Event{
		HookEventName:  "preToolUse",
		ConversationID: "cursor-conversation",
	}
	if !eventMatchesHost(hostCursor, &cursorEvent) {
		t.Fatal("Cursor adapter should accept Cursor payloads")
	}
	if eventMatchesHost(hostClaude, &cursorEvent) {
		t.Fatal("Claude adapter must ignore Cursor compatibility payloads")
	}

	claudeEvent := Event{HookEventName: "PreToolUse", SessionID: "claude-session"}
	if !eventMatchesHost(hostClaude, &claudeEvent) {
		t.Fatal("Claude adapter should accept Claude payloads")
	}
	if eventMatchesHost(hostCursor, &claudeEvent) {
		t.Fatal("Cursor adapter must ignore Claude payloads")
	}
}

func TestEmitSessionEnv(t *testing.T) {
	input := strings.NewReader(`{"session_id":"cursor-session"}`)
	var output bytes.Buffer
	if err := emitSessionEnv(input, &output); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Env["PLANNOTATOR_REVIEW_GATE_SESSION_ID"] != "cursor-session" {
		t.Fatalf("unexpected environment response: %+v", got)
	}
}

func TestParseHost(t *testing.T) {
	for _, value := range []string{"claude", "codex", "cursor"} {
		if _, err := parseHost(value); err != nil {
			t.Errorf("parseHost(%q): %v", value, err)
		}
	}
	if _, err := parseHost("other"); err == nil {
		t.Fatal("unknown host should fail")
	}
}

func TestSessionCommandDecision(t *testing.T) {
	ev := Event{
		host:      hostCursor,
		SessionID: "cursor-session",
		ToolName:  "Shell",
		ToolInput: json.RawMessage(`{
			"command":"plannotator-review-gate on --skip-tests",
			"working_directory":"/workspace"
		}`),
	}
	d := sessionCommandDecision(hostCursor, &ev)
	if d == nil || d.Permission != "allow" {
		t.Fatalf("expected rewritten allow decision, got %+v", d)
	}
	command, _ := d.UpdatedInput["command"].(string)
	if !strings.Contains(command, "PLANNOTATOR_REVIEW_GATE_SESSION_ID='cursor-session'") ||
		d.UpdatedInput["working_directory"] != "/workspace" {
		t.Fatalf("unexpected rewritten input: %+v", d.UpdatedInput)
	}

	ev.ToolName = "Write"
	if d := sessionCommandDecision(hostCursor, &ev); d != nil {
		t.Fatalf("non-shell edit should not be rewritten: %+v", d)
	}
}

func TestHookConfigs(t *testing.T) {
	cases := []struct {
		host host
		want []string
	}{
		{hostClaude, []string{"hook claude", "Edit|Write|MultiEdit"}},
		{hostCodex, []string{"hook codex", "Bash|Edit|Write", "statusMessage"}},
		{hostCursor, []string{"hook cursor", "Shell|Write", "failClosed"}},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(hookConfig(tc.host))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range tc.want {
			if !bytes.Contains(raw, []byte(want)) {
				t.Errorf("%s config missing %q: %s", tc.host, want, raw)
			}
		}
	}
}
