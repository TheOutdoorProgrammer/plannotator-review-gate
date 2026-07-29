package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Plannotator has no structured review output, so the verdict is read from
// stdout text: this closed marker, the approved phrase, or anything else
// (treated as reviewer feedback).
const (
	reviewClosedMarker   = "Review session closed without feedback."
	reviewApprovedMarker = "no changes requested"
)

// stageRepo builds a throwaway git repo whose only unstaged change is the
// proposed edit: HEAD holds the current content (or intent-to-add for a new
// file), the worktree holds the proposed content, keeping the real rel path.
func stageRepo(repo string, ch *change, cwd string) error {
	rel := filepath.Base(ch.path)
	if cwd != "" {
		if r, err := filepath.Rel(cwd, ch.path); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	target := filepath.Join(repo, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	run := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		return cmd.Run()
	}
	if err := run("init", "-q"); err != nil {
		return err
	}
	// A fresh `git init` repo is NOT isolated — it inherits your global config.
	// Pin these locally (local beats global, and it also covers git run by
	// Plannotator in this repo): inherited commit signing costs a hardware-key
	// tap per edit, and a global pre-commit hook can fail the staging commit,
	// which would fail the gate on every change.
	for _, kv := range [][2]string{
		{"user.email", "review-gate@localhost"},
		{"user.name", "review-gate"},
		{"commit.gpgsign", "false"},
		{"tag.gpgsign", "false"},
		{"core.hooksPath", filepath.Join(repo, ".git", "no-hooks")},
	} {
		_ = run("config", kv[0], kv[1])
	}

	if ch.exists {
		if err := os.WriteFile(target, []byte(ch.before), 0o644); err != nil {
			return err
		}
		if err := run("add", rel); err != nil {
			return err
		}
		if err := run("commit", "-q", "-m", "current"); err != nil {
			return err
		}
		return os.WriteFile(target, []byte(ch.after), 0o644)
	}
	// New file: commit empty, then intent-to-add so it shows in the unstaged diff.
	if err := run("commit", "-q", "--allow-empty", "-m", "empty"); err != nil {
		return err
	}
	if err := os.WriteFile(target, []byte(ch.after), 0o644); err != nil {
		return err
	}
	return run("add", "-N", rel)
}

// runPlannotator opens the staged repo in Plannotator's review UI: allow on
// approval, deny carrying feedback, nil when the user dismisses it, broken-gate
// deny when no verdict comes back at all.
func runPlannotator(ctx context.Context, repo string) *Decision {
	bin := plannotatorBin()
	if bin == "" {
		return gateBroken("plannotator binary not found on PATH or in ~/.local/bin")
	}
	cmd := exec.CommandContext(ctx, bin, "review")
	cmd.Dir = repo
	out, err := cmd.Output()
	d, dismissed := decisionFromReview(string(out))
	switch {
	case d != nil:
		return d
	case dismissed:
		return nil
	default:
		return gateBroken(fmt.Sprintf("plannotator returned no verdict (%v)", err))
	}
}

// decisionFromReview maps Plannotator's review stdout to a verdict. dismissed
// is true only for the explicit closed marker, so a silent Plannotator is
// never mistaken for the user waving the change through.
func decisionFromReview(out string) (d *Decision, dismissed bool) {
	text := strings.TrimSpace(out)
	if strings.Contains(text, reviewClosedMarker) {
		return nil, true
	}
	if text == "" {
		return nil, false
	}
	if strings.Contains(text, reviewApprovedMarker) {
		return &Decision{Permission: "allow", Reason: "Approved by the user in the Plannotator review gate."}, false
	}
	return &Decision{
		Permission: "deny",
		Reason: "The user reviewed this proposed change in the Plannotator review gate " +
			"and left feedback before it was applied. The change has NOT been made. " +
			"Address the feedback, then propose the edit again:\n\n" + text,
	}, false
}

// plannotatorBin resolves the plannotator CLI. Hooks can run with a minimal
// PATH, so fall back to the known install location.
func plannotatorBin() string {
	if found, err := exec.LookPath("plannotator"); err == nil {
		return found
	}
	if home, err := os.UserHomeDir(); err == nil {
		fallback := filepath.Join(home, ".local", "bin", "plannotator")
		if _, err := os.Stat(fallback); err == nil {
			return fallback
		}
	}
	return ""
}
