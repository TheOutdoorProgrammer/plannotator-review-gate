---
description: Toggle the Plannotator review gate for THIS session — review every Edit/Write in the code-review UI before it lands.
---

Run the review-gate toggle with the user's argument (default to `toggle` if none given):

```bash
plannotator-review-gate $ARGUMENTS
```

Valid arguments: `on`, `off`, `toggle`, `status`, optionally with `--skip-tests`.

`--skip-tests` (e.g. `/review-gate on --skip-tests`) enables the gate but leaves
**test files ungated** — matched by naming convention (Go `*_test.go`, pytest
`test_*.py`/`conftest.py`, `*.test`/`*.spec.{js,ts,…}`, `*Test.java`, a
`__tests__/` dir). Useful when you don't need to review generated/updated tests.
The option is stored per session; `status` reports whether it's active.

The gate is toggled **per Claude Code session**. `/review-gate on` enables it for
the current session only — other running Claude Code instances are unaffected.
State is keyed off `CLAUDE_CODE_SESSION_ID` (present in the session's env); if
that's missing the command errors rather than guessing which session to act on.

Report the command's output back to the user verbatim. The per-session flag is
checked at hook runtime, so the change takes effect on the very next Edit/Write —
no restart needed.

While the gate is **enabled**: every Edit/Write/MultiEdit you propose opens
in Plannotator's code-review UI (side-by-side diff, line comments). If it
comes back denied with reviewer feedback, the change was NOT applied — revise
it per the feedback and propose the edit again. Do not retry the identical
edit, and do not work around the gate by writing files via Bash.

While the gate is enabled, **narrate the edits worth narrating**.
Before making an edit, queue an explanation and the gate posts it into that edit's review, pinned to the line:

```bash
plannotator-review-gate note <file> [--line N | --line N-M] \
  [--type comment|suggestion|concern] "why this change looks like this"
```

Use it where the diff can't explain itself — a non-obvious workaround, a decision and the alternative you rejected, something you want challenged (`--type concern`), a gap you knowingly left.
Skip it for mechanical edits; a note per hunk is noise.

**Put each note on the line it's about.** `--line N` is the line number *after* your edit lands — the line you actually changed, not the top of the file.
If a note isn't about one place in the file, omit `--line` and it posts as a review-level comment; that is what whole-change remarks are for.
Never use `--line 1` as a stand-in for "somewhere in here", and don't guess a number either — a line that isn't in the diff lands detached in the sidebar, which is where an unpinned note goes anyway, minus the honesty.
The queue enforces this. An unpinned note (no `--line`, or `--line 1`) is rejected when it runs past 3 lines or 400 characters, and a *second* unpinned note for the same file is rejected outright.
Both rejections exit non-zero and tell you to split it up: pin each piece to the line it explains, or fold it into the one review-level note.
Queue notes **before** the edit, since the gate drains them when that edit is reviewed.
Do **not** POST to Plannotator's annotation API yourself during a gated edit: those annotations come back as the reviewer's feedback and deny your own edit, and the hook blocks you for the review's whole life anyway.

The gate **fails closed**: if it can't stage the edit or can't open Plannotator
at all, the edit is denied rather than quietly applied unreviewed. That denial
names the cause and is not something to code around — stop and tell the user, who
decides whether to fix the gate or turn it off. Never run `plannotator-review-gate off`
yourself to get past it.
