package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

const reviewGateLockPrefix = "🔒 "

// updateReviewGateSidebar puts a lock on the current cmux workspace tab while
// the gate is on. Best-effort and a no-op outside cmux — never affects the
// toggle.
func updateReviewGateSidebar(enabled bool) {
	ref := cmuxFocusedWorkspace()
	if ref == "" {
		return
	}
	base := strings.TrimPrefix(cmuxWorkspaceTitle(ref), reviewGateLockPrefix)
	if base == "" {
		return
	}
	title := base
	if enabled {
		title = reviewGateLockPrefix + base
	}
	_ = cmuxCmd("workspace-action", "--action", "rename", "--workspace", ref, "--title", title).Run()
}

// cmuxFocusedWorkspace returns the caller's focused workspace ref, or "" if not
// inside cmux.
func cmuxFocusedWorkspace() string {
	out, err := cmuxCmd("identify").Output()
	if err != nil {
		return ""
	}
	var r struct {
		Focused struct {
			WorkspaceRef string `json:"workspace_ref"`
		} `json:"focused"`
	}
	if json.Unmarshal(out, &r) != nil {
		return ""
	}
	return r.Focused.WorkspaceRef
}

func cmuxWorkspaceTitle(ref string) string {
	out, err := cmuxCmd("workspace", "list", "--json").Output()
	if err != nil {
		return ""
	}
	var payload struct {
		Workspaces []struct {
			Ref   string `json:"ref"`
			Title string `json:"title"`
		} `json:"workspaces"`
	}
	if json.Unmarshal(out, &payload) != nil {
		return ""
	}
	for _, w := range payload.Workspaces {
		if w.Ref == ref {
			return w.Title
		}
	}
	return ""
}

// cmuxCmd builds a cmux CLI invocation with the deprecation chatter silenced.
func cmuxCmd(args ...string) *exec.Cmd {
	cmd := exec.Command("cmux", args...)
	cmd.Env = append(os.Environ(), "CMUX_QUIET=1")
	return cmd
}
