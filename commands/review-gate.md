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

The gate **fails closed**: if it can't stage the edit or can't open Plannotator
at all, the edit is denied rather than quietly applied unreviewed. That denial
names the cause and is not something to code around — stop and tell the user, who
decides whether to fix the gate or turn it off. Never run `plannotator-review-gate off`
yourself to get past it.
