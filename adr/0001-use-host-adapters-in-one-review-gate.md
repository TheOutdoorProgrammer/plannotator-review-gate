# 1. Use host adapters in one review gate

Date: 2026-10-08

## Status

Accepted.

## Context and Problem Statement

The review gate currently supports only Claude Code, while development moves between Claude Code, Codex, and Cursor.
Each host exposes different event names, session identifiers, edit formats, response envelopes, and failure behavior.
Human review should remain opt-in per session and present the same Plannotator diff workflow everywhere.

## Considered Options

1. Extend the existing review-gate binary with explicit Claude, Codex, and Cursor adapters
2. Keep the Claude per-edit gate and use each other host's native checkpoint review
3. Run manual Plannotator worktree reviews at checkpoints in every host

## Decision Outcome

Keep staging, filtering, notes, and Plannotator verdict handling in one binary. Add explicit host adapters at the input, edit-normalization, response, session-command, and generated-wiring boundaries. Preserve the no-argument Claude hook entrypoint for compatibility, while generated configuration names each host explicitly. Cursor wiring sets `failClosed` and explicitly allows deferred tool calls; Codex receives valid deny responses for every detected gate failure, while documenting that Codex itself still fails open if the hook process crashes or times out.

## Consequences

### Good

- One toggle and review vocabulary works across all three local agents
- Filtering, security checks, queued notes, and verdict handling stay in one implementation
- Host-specific behavior is isolated and testable instead of leaking through the gate

### Bad

- Every host hook schema is now a compatibility surface this project must track
- Codex can still fail open on a process crash, timeout, or malformed response because the host does not offer a fail-closed hook option
- Cursor user hooks do not cover Cloud Agents, so the local gate cannot promise universal Cursor enforcement
- Atomic multi-file patch staging and path validation increase implementation complexity

### Rejected because

- Native review differs across hosts, is generally post-edit or checkpoint-based, and does not provide the requested consistent line-feedback gate before edits land
- Manual checkpoint review is portable but reviews changes only after they have landed and relies on agents and users remembering to invoke it
