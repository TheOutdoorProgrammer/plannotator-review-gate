package main

import (
	"context"
	"fmt"
	"os"
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
	if ev.SessionID == "" || !enabled || !ev.isGatedTool() {
		return nil
	}
	// Keep the flag fresh so pruneStaleFlags doesn't reap an active session.
	now := time.Now()
	_ = os.Chtimes(flagPath(ev.SessionID), now, now)

	changes, err := proposedChanges(ev.ToolName, ev.ToolInput, ev.CWD)
	if err != nil {
		return gateBroken(fmt.Sprintf("could not preview the proposed edit (%v)", err))
	}
	changes = reviewableChanges(changes, opts)
	if len(changes) == 0 {
		return nil
	}

	repo, err := os.MkdirTemp("", "review-gate-")
	if err != nil {
		return gateBroken(fmt.Sprintf("could not create the staging repo (%v)", err))
	}
	defer func() { _ = os.RemoveAll(repo) }()

	if err := stageRepo(repo, changes, ev.CWD); err != nil {
		return gateBroken(fmt.Sprintf("could not stage the edit for review (%v)", err))
	}

	remaining := loadNotes(ev.SessionID)
	var notesByFile []reviewNotes
	for _, ch := range changes {
		var mine []note
		mine, remaining = notesFor(remaining, ch.path)
		if len(mine) > 0 {
			notesByFile = append(notesByFile, reviewNotes{
				rel:   stagedRelPath(ch.path, ev.CWD),
				notes: mine,
			})
		}
	}
	d, posted := runPlannotator(ctx, repo, notesByFile)
	if posted {
		if err := saveNotes(ev.SessionID, remaining); err != nil {
			fmt.Fprintf(os.Stderr, "plannotator-review-gate: could not clear posted notes (%v)\n", err)
		}
	}
	return d
}

func reviewableChanges(changes []change, opts map[string]bool) []change {
	out := make([]change, 0, len(changes))
	for _, ch := range changes {
		if ch.before == ch.after && !ch.deleted {
			continue
		}
		if opts["skip-tests"] && isTestFile(ch.path) {
			continue
		}
		if skipPath(ch.path) {
			continue
		}
		if isWhitespaceOnlyChange(ch.before, ch.after) {
			continue
		}
		if ch.exists && !ch.deleted && isCommentOnlyChange(ch.path, ch.before, ch.after) {
			continue
		}
		out = append(out, ch)
	}
	return out
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
