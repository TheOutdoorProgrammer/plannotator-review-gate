package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Event is the hook payload Claude Code writes to stdin. Fields absent for a
// given event stay zero.
type Event struct {
	HookEventName  string          `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	CWD            string          `json:"cwd"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
}

// Decision is the gate's verdict; a nil *Decision means defer to the
// normal permission flow.
type Decision struct {
	Permission string // "allow" or "deny"
	Reason     string // shown to the model; multiline is fine
}

// gatedTools are the file-writing tools worth reviewing. Claude Code matches
// hooks by tool name in settings.json, but it is matched here too so a broad
// "*" matcher costs nothing on unrelated tools.
var gatedTools = []string{"Edit", "Write", "MultiEdit"}

// respondDecision builds the PreToolUse response envelope. Claude Code parses
// hook stdout as JSON and ignores anything else.
func respondDecision(d *Decision) (string, error) {
	envelope := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       d.Permission,
			"permissionDecisionReason": d.Reason,
		},
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encoding decision envelope: %w", err)
	}
	return string(raw), nil
}

// printHookConfig prints the settings.json entry to wire this binary up. The
// timeout must be at least reviewGateTimeout: it is how long Claude Code will
// wait for the hook, and therefore how long you have to finish a review.
func printHookConfig() {
	fmt.Println("# Add to \"hooks\" in ~/.claude/settings.json (append to an existing")
	fmt.Println("# PreToolUse array — multiple hooks per event compose):")
	fmt.Println()
	// Structs, not maps: encoding/json sorts map keys, and a settings snippet
	// people paste should read in its natural order.
	type hookCmd struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}
	type matcherEntry struct {
		Matcher string    `json:"matcher"`
		Hooks   []hookCmd `json:"hooks"`
	}
	entry := struct {
		PreToolUse []matcherEntry `json:"PreToolUse"`
	}{[]matcherEntry{{
		Matcher: strings.Join(gatedTools, "|"),
		Hooks:   []hookCmd{{Type: "command", Command: binaryPath(), Timeout: reviewGateTimeout}},
	}}}
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "plannotator-review-gate: %v\n", err)
		return
	}
	fmt.Println(string(raw))
	fmt.Println()
	fmt.Printf("# The %ds timeout is how long a review may take before Claude Code\n", reviewGateTimeout)
	fmt.Println("# gives up on the hook. Lower it and long reviews get cut off.")
}

// binaryPath resolves this executable's real path for the wiring snippet, so
// the printed config points at wherever the user actually installed it.
func binaryPath() string {
	p, err := os.Executable()
	if err != nil {
		return "plannotator-review-gate"
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}
