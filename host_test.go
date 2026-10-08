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

func TestHookConfigs(t *testing.T) {
	cases := []struct {
		host host
		want []string
	}{
		{hostClaude, []string{"hook claude", "Edit|Write|MultiEdit"}},
		{hostCodex, []string{"hook codex", "Edit|Write", "statusMessage"}},
		{hostCursor, []string{"session-env", "hook cursor", "failClosed"}},
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
