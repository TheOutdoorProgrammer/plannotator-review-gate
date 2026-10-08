package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type host string

const (
	hostClaude host = "claude"
	hostCodex  host = "codex"
	hostCursor host = "cursor"
)

func parseHost(value string) (host, error) {
	switch host(strings.ToLower(value)) {
	case hostClaude:
		return hostClaude, nil
	case hostCodex:
		return hostCodex, nil
	case hostCursor:
		return hostCursor, nil
	default:
		return "", fmt.Errorf("unsupported host %q (want claude, codex, or cursor)", value)
	}
}

func normalizeEvent(h host, ev *Event) {
	ev.host = h
	if ev.SessionID == "" {
		ev.SessionID = ev.ConversationID
	}
	if ev.HookEventName == "preToolUse" {
		ev.HookEventName = "PreToolUse"
	}
}

func eventMatchesHost(h host, ev *Event) bool {
	switch h {
	case hostClaude:
		return ev.ConversationID == "" && ev.HookEventName != "preToolUse"
	case hostCursor:
		return ev.ConversationID != "" || ev.HookEventName == "preToolUse"
	default:
		return ev.ConversationID == ""
	}
}

func respondDecision(h host, d *Decision) (string, error) {
	var envelope any
	if h == hostCursor {
		output := map[string]any{
			"permission":    d.Permission,
			"user_message":  cursorUserMessage(d),
			"agent_message": d.Reason,
		}
		if d.UpdatedInput != nil {
			output["updated_input"] = d.UpdatedInput
		}
		envelope = output
	} else {
		output := map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       d.Permission,
			"permissionDecisionReason": d.Reason,
		}
		if d.UpdatedInput != nil {
			output["updatedInput"] = d.UpdatedInput
		}
		envelope = map[string]any{
			"hookSpecificOutput": output,
		}
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("encoding %s decision: %w", h, err)
	}
	return string(raw), nil
}

func cursorUserMessage(d *Decision) string {
	if d.Permission == "allow" {
		return ""
	}
	return "The proposed edit was denied by the Plannotator review gate. Feedback was returned to the agent."
}

func emitSessionEnv(r io.Reader, w io.Writer) error {
	var ev Event
	if err := json.NewDecoder(r).Decode(&ev); err != nil {
		return fmt.Errorf("decoding sessionStart event: %w", err)
	}
	sessionID := ev.SessionID
	if sessionID == "" {
		sessionID = ev.ConversationID
	}
	if sessionID == "" {
		return fmt.Errorf("sessionStart event has no session identifier")
	}
	return json.NewEncoder(w).Encode(map[string]any{
		"env": map[string]string{"PLANNOTATOR_REVIEW_GATE_SESSION_ID": sessionID},
	})
}

func sessionCommandDecision(h host, ev *Event) *Decision {
	isShell := h == hostCursor && ev.ToolName == "Shell" ||
		h == hostCodex && ev.ToolName == "Bash"
	if !isShell || ev.SessionID == "" {
		return nil
	}
	var input map[string]any
	if err := json.Unmarshal(ev.ToolInput, &input); err != nil {
		return nil
	}
	command, ok := input["command"].(string)
	if !ok {
		return nil
	}
	fields := strings.Fields(command)
	if len(fields) < 2 || filepath.Base(fields[0]) != "plannotator-review-gate" ||
		!isSessionCommand(fields[1]) {
		return nil
	}
	input["command"] = "PLANNOTATOR_REVIEW_GATE_SESSION_ID=" +
		shellQuote(ev.SessionID) + " " + command
	return &Decision{Permission: "allow", UpdatedInput: input}
}

func isSessionCommand(value string) bool {
	switch value {
	case "on", "off", "toggle", "status", "note":
		return true
	default:
		return false
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func printHookConfig(h host) {
	intro := "Merge into ~/.claude/settings.json under hooks:"
	switch h {
	case hostCodex:
		intro = "Merge into ~/.codex/hooks.json:"
	case hostCursor:
		intro = "Merge into ~/.cursor/hooks.json:"
	}
	printConfig(intro, hookConfig(h))
}

func hookConfig(h host) map[string]any {
	switch h {
	case hostCodex:
		return map[string]any{
			"description": "Plannotator review gate",
			"hooks": map[string]any{
				"PreToolUse": []any{map[string]any{
					"matcher": "Bash|Edit|Write",
					"hooks": []any{map[string]any{
						"type":          "command",
						"command":       binaryPath() + " hook codex",
						"timeout":       reviewGateTimeout,
						"statusMessage": "Waiting for Plannotator review",
					}},
				}},
			},
		}
	case hostCursor:
		return map[string]any{
			"version": 1,
			"hooks": map[string]any{
				"preToolUse": []any{map[string]any{
					"command":    binaryPath() + " hook cursor",
					"matcher":    "Shell|Write|Delete",
					"timeout":    reviewGateTimeout,
					"failClosed": true,
				}},
			},
		}
	default:
		return map[string]any{
			"PreToolUse": []any{map[string]any{
				"matcher": "Edit|Write|MultiEdit",
				"hooks": []any{map[string]any{
					"type":    "command",
					"command": binaryPath() + " hook claude",
					"timeout": reviewGateTimeout,
				}},
			}},
		}
	}
}

func printConfig(intro string, config any) {
	fmt.Println(intro)
	fmt.Println()
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "plannotator-review-gate: %v\n", err)
		return
	}
	fmt.Println(string(raw))
	fmt.Println()
	fmt.Printf("The %ds timeout is the review deadline; do not lower it.\n", reviewGateTimeout)
}

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
