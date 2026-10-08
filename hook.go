package main

import (
	"encoding/json"
)

// Event is the union of the hook payload fields used across supported hosts.
type Event struct {
	host           host            `json:"-"`
	HookEventName  string          `json:"hook_event_name"`
	SessionID      string          `json:"session_id"`
	ConversationID string          `json:"conversation_id"`
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

func (e *Event) isGatedTool() bool {
	switch e.host {
	case hostCodex:
		return e.ToolName == "apply_patch"
	case hostCursor:
		return e.ToolName == "Write" || e.ToolName == "Delete" ||
			e.ToolName == "ApplyPatch" || e.ToolName == "apply_patch"
	default:
		return e.ToolName == "Edit" || e.ToolName == "Write" || e.ToolName == "MultiEdit"
	}
}
