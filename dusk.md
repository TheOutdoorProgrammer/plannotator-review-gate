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

A `PreToolUse` hook for Claude Code, Codex CLI, and Cursor CLI that turns the permission prompt for an edit into an actual terminal code review.
With the gate on, Claude and Cursor `Edit`, `Write`, or `MultiEdit` calls and Codex `apply_patch` calls are staged in a throwaway git repository and rendered as a Markdown unified diff in Plannotator TUI.
A single Codex patch may add, update, delete, or move multiple files; the gate reviews it as one atomic diff.
Submitting with no annotations, or only looks-good annotations, applies the edit.
Submitting comments or delete annotations denies it, so the files are never written and the feedback goes back to the model.
Closing the TUI without submitting defers to the normal permission flow.

The gate is opt-in per agent session. Claude uses `CLAUDE_CODE_SESSION_ID`, Codex uses `CODEX_SESSION_ID`, and Cursor receives `PLANNOTATOR_REVIEW_GATE_SESSION_ID` from the gate's `sessionStart` hook using its conversation id.
Flags remain under `~/.claude/plannotator-review-gate.sessions/` for backward compatibility and stale ones are swept after seven days.
`plannotator-review-gate hook-config` prints mergeable hook snippets for all three agents, and its long timeout is the review deadline rather than decoration: lowering it cuts reviews off mid-thought.
Releases are tag-driven, with a `vX.Y.Z` tag running the tests and then GoReleaser.

The source is flat at the repository root, roughly one file per concern.
`gate.go` makes the decision, `change.go` normalizes Claude and Cursor edits, `codex_patch.go` previews Codex patches with the session's `apply_patch` helper, `filter.go` and `comments.go` decide what is not worth reviewing, `stage.go` builds the temporary repository and launches the TUI, `terminal.go` bridges it through a pseudo-terminal, `tui.go` owns its submission record contract, `notes.go` carries the agent's context, and `sidebar.go` is the tab indicator.

## Gotchas

**It fails closed.**
If it cannot preview or stage the edit, open the controlling terminal, launch Plannotator TUI, read the submission record, or export feedback, it denies rather than quietly applying.
The denial explicitly tells the model not to retry, write the file some other way, or turn the gate off.
The single exception is a crash in this program on a session with the gate *off*, which exits 0, because a bug here must never wedge a session that never asked for review.

**Codex patch previews use Codex's own helper.**
The custom `apply_patch` format is not parsed as a hand-rolled approximation.
The gate copies only the named regular files into an isolated directory, rejects lexical and symlink path escapes, runs the session's `apply_patch` helper there, then stages the resulting file set.
If that helper is missing or rejects the patch, an enabled gate fails closed.

**The TUI must not deliver directly to Herdr.**
The agent is blocked inside the hook while the review runs, so prompting the same pane cannot carry the verdict back to the hook.
The gate clears Herdr delivery variables and gives each review an isolated `PLANNOTATOR_DATA_DIR`; pressing `E` records the submission locally for the hook to read after the TUI closes.
Standalone Plannotator TUI also writes every submitted review to the terminal clipboard through OSC 52.
The PTY bridge filters only that control sequence because the gate reads the same submission from the isolated record; the TUI still says Copied, but the clipboard is unchanged.

**A stale or malformed submission fails closed.**
No delivery record means the reviewer quit before pressing `E`, which intentionally returns to the normal permission flow.
Once a delivery exists, malformed timestamps, changed annotations, or mismatched annotation IDs deny the edit instead of masquerading as a dismissal.

**Queued agent notes are document content, not reviewer annotations.**
They render above the diff and are removed from the queue once shown, so they cannot be mistaken for human feedback.

**`sidebar.go` is inert.**
The lock indicator only ever spoke cmux, which is retired, so on a current machine it paints nothing.
`plannotator-review-gate status` is the only trustworthy answer to whether a session is gated.
