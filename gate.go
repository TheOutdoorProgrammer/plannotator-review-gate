package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"
)

// reviewGateTimeout is the gate's wall-clock budget in seconds: effectively
// "wait as long as the reviewer needs" (4 days). It bounds the Plannotator
// process and doubles as the settings.json hook timeout (see printHookConfig).
const reviewGateTimeout = 345600

func gateContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), reviewGateTimeout*time.Second)
}

// gateReviewGate opens the proposed edit in Plannotator's review UI and returns
// the reviewer's verdict, deferring (nil) whenever review would be noise or
// does not apply.
func gateReviewGate(ctx context.Context, ev *Event) *Decision {
	opts, enabled := gateOpts(ev.SessionID)
	if ev.SessionID == "" || !enabled || !slices.Contains(gatedTools, ev.ToolName) {
		return nil
	}
	// Keep the flag fresh so pruneStaleFlags doesn't reap an active session.
	now := time.Now()
	_ = os.Chtimes(flagPath(ev.SessionID), now, now)

	ch := proposedChange(ev.ToolName, ev.ToolInput, ev.CWD)
	if ch == nil || ch.before == ch.after {
		return nil
	}
	if opts["skip-tests"] && isTestFile(ch.path) {
		return nil
	}
	if skipPath(ch.path) {
		return nil
	}
	// Review noise: pure whitespace/blank-line churn (any file type), or a
	// comment-only edit to an existing file of a known language.
	if isWhitespaceOnlyChange(ch.before, ch.after) {
		return nil
	}
	if ch.exists && isCommentOnlyChange(ch.path, ch.before, ch.after) {
		return nil
	}

	repo, err := os.MkdirTemp("", "review-gate-")
	if err != nil {
		return gateBroken(fmt.Sprintf("could not create the staging repo (%v)", err))
	}
	defer func() { _ = os.RemoveAll(repo) }()

	if err := stageRepo(repo, ch, ev.CWD); err != nil {
		return gateBroken(fmt.Sprintf("could not stage the edit for review (%v)", err))
	}
	return runPlannotator(ctx, repo)
}

// gateBroken fails the gate CLOSED: an unreviewed edit landing while the user
// believes every edit is reviewed is worse than a stuck session, and the model
// must not paper over it (or disable the gate) on its own.
func gateBroken(detail string) *Decision {
	fmt.Fprintf(os.Stderr, "plannotator-review-gate: %s\n", detail)
	return &Decision{
		Permission: "deny",
		Reason: "The Plannotator review gate is ON for this session but could not review this edit: " +
			detail + ". The edit was NOT made and NOT reviewed. Do not retry it, do not edit around " +
			"the gate, and do NOT turn the gate off — stop and tell the user, who can fix the gate or " +
			"run `plannotator-review-gate off` themselves.",
	}
}
