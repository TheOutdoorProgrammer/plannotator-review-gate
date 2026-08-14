---
dusk: v1alpha1
namespace: stout
kind: repository
name: plannotator-review-gate
title: Plannotator Review Gate
attributes:
  language: go
  license: MIT
  public: true
---

A Claude Code `PreToolUse` hook that turns the permission prompt for an edit into an actual code review.
With the gate on, a proposed `Edit`, `Write` or `MultiEdit` is staged into a throwaway git repository so there is a real diff to look at, and that diff opens in Plannotator's review UI.
Approving applies the edit.
Leaving line comments denies it, so the file is never written, and the comments go back to the model as feedback to revise against.

The gate is per session, keyed on `CLAUDE_CODE_SESSION_ID`, with flags under `~/.claude/plannotator-review-gate.sessions/` and stale ones swept after seven days, so one terminal can review every edit while another works untouched.
`plannotator-review-gate hook-config` prints the settings entry to install, and its long timeout is the review deadline rather than decoration: lowering it cuts reviews off mid-thought.
Releases are tag-driven, with a `vX.Y.Z` tag running the tests and then GoReleaser.

The source is flat at the repository root, roughly one file per concern.
`gate.go` makes the decision, `filter.go` and `comments.go` decide what is not worth reviewing, `stage.go` builds the temporary repository, `review_api.go` talks to Plannotator, `notes.go` carries the agent's own annotations, and `sidebar.go` is the tab indicator.

## Gotchas

**It fails closed.**
If it cannot stage the edit or cannot launch Plannotator it denies rather than quietly applying, and the denial explicitly tells the model not to retry, not to write the file some other way, and not to turn the gate off.
The single exception is a crash in this program on a session with the gate *off*, which exits 0, because a bug here must never wedge a session that never asked for review.

**Posted notes have to be stripped back out of the verdict.**
Plannotator merges externally posted annotations into the reviewer's own set and exports them with no source marker, so a note the gate posted would come back as the reviewer's feedback and deny the very edit it was explaining.
Editing a note is how a reviewer adopts it: an edited note stops matching and does reach the model.

**`sidebar.go` is inert.**
The lock indicator only ever spoke cmux, which is retired, so on a current machine it paints nothing.
`plannotator-review-gate status` is the only trustworthy answer to whether a session is gated.
