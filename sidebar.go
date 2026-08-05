package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const reviewGateLockPrefix = "🔒 "

// noSidebarFile remembers that cmux isn't installed, so the probe runs once per
// machine instead of once per toggle. Delete it to re-check (after installing
// cmux), or create it by hand to opt out of the indicator entirely.
const noSidebarFile = "plannotator-review-gate.no-sidebar"

func noSidebarPath() string {
	return filepath.Join(claudeDir(), noSidebarFile)
}

// updateReviewGateSidebar locks the current cmux workspace tab while the gate is
// on; best-effort, never affects the toggle. INERT since cmux was retired for
// muxy without a port — every toggle now takes the rememberNoCmux path.
func updateReviewGateSidebar(enabled bool) {
	if _, err := os.Stat(noSidebarPath()); err == nil {
		return
	}
	// Cache only the durable fact — cmux absent from this machine. A cmux that
	// IS installed but reports no workspace just means this shell isn't a cmux
	// session (a plain terminal, ssh, CI); caching that would kill the
	// indicator inside cmux too, until someone found the file and deleted it.
	if _, err := exec.LookPath("cmux"); err != nil {
		rememberNoCmux()
		return
	}
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

// rememberNoCmux writes the marker, with its own instructions inside so whoever
// finds it knows what it does. Best-effort: failing to cache is harmless.
func rememberNoCmux() {
	body := "cmux was not found on PATH, so the plannotator-review-gate sidebar\n" +
		"indicator is disabled and no longer probed for.\n\n" +
		"Delete this file to re-check (e.g. after installing cmux).\n" +
		"Recreate it to opt out of the indicator even when cmux IS installed.\n"
	_ = os.WriteFile(noSidebarPath(), []byte(body), 0o644)
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
