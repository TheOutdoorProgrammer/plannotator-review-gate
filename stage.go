package main

import (
	"bytes"
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

// stagedRelPath is the path the staged diff will show for this edit, which is
// also the filePath a line annotation must carry to pin to its line.
func stagedRelPath(path, cwd string) string {
	if cwd != "" {
		if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return filepath.Base(path)
}

// stageRepo builds a throwaway git repo whose only unstaged change is the
// proposed edit: HEAD holds the current content (or intent-to-add for a new
// file), the worktree holds the proposed content, keeping the real rel path.
func stageRepo(repo string, ch *change, cwd string) error {
	rel := stagedRelPath(ch.path, cwd)
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
// deny when no verdict comes back at all. posted reports whether ns was shown.
func runPlannotator(ctx context.Context, repo, rel string, ns []note) (d *Decision, posted bool) {
	bin := plannotatorBin()
	if bin == "" {
		return gateBroken("plannotator binary not found on PATH or in ~/.local/bin"), false
	}
	cmd := exec.CommandContext(ctx, bin, "review")
	cmd.Dir = repo
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		return gateBroken(fmt.Sprintf("could not start plannotator (%v)", err)), false
	}
	if len(ns) > 0 {
		posted = narrate(ctx, filepath.Base(repo), rel, ns)
	}

	err := cmd.Wait()
	d, dismissed := verdictFromReview(stdout.String(), ns, posted)
	switch {
	case d != nil:
		return d, posted
	case dismissed:
		return nil, posted
	default:
		return gateBroken(fmt.Sprintf("plannotator returned no verdict (%v)", err)), posted
	}
}

// verdictFromReview strips the gate's own narration out of the review output
// before reading it, so posted notes can never be mistaken for the reviewer's
// feedback.
func verdictFromReview(out string, ns []note, posted bool) (*Decision, bool) {
	if !posted {
		return decisionFromReview(out)
	}
	out = stripNotes(out, ns)
	// A review whose only comments were ours is not an approval, and the gate
	// never lets an edit through unreviewed.
	if !strings.Contains(out, reviewClosedMarker) && !isApproved(out) &&
		strings.TrimSpace(out) != "" && !reviewerLeftContent(out) {
		return &Decision{
			Permission: "deny",
			Reason: "The user submitted the Plannotator review without leaving any feedback " +
				"of their own — the only comments in it were your own notes. The change has " +
				"NOT been made. Ask what they want changed rather than re-proposing blindly.",
		}, false
	}
	return decisionFromReview(out)
}

// narrate posts the queued notes into the review that was just opened. This is
// the one place the gate does NOT fail closed: an unposted comment costs
// nothing, whereas failing an edit over cosmetics would be maddening.
func narrate(ctx context.Context, project, rel string, ns []note) bool {
	baseURL, ok := findReviewSession(ctx, project)
	if !ok {
		fmt.Fprintln(os.Stderr, "plannotator-review-gate: review session never registered — notes not posted")
		return false
	}
	if !waitForDiffReady(ctx, baseURL) {
		fmt.Fprintln(os.Stderr, "plannotator-review-gate: review has no origin — notes would be dropped")
		return false
	}
	if err := postNotes(ctx, baseURL, rel, ns); err != nil {
		fmt.Fprintf(os.Stderr, "plannotator-review-gate: %v\n", err)
		return false
	}
	return true
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
	if isApproved(text) {
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
