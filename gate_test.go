package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecisionFromReview(t *testing.T) {
	// No output is NOT a dismissal — the caller must fail the gate closed.
	if d, dismissed := decisionFromReview(""); d != nil || dismissed {
		t.Errorf("empty output: want (nil, false), got (%+v, %v)", d, dismissed)
	}
	if d, dismissed := decisionFromReview(reviewClosedMarker); d != nil || !dismissed {
		t.Errorf("closed marker: want (nil, true), got (%+v, %v)", d, dismissed)
	}
	if d, _ := decisionFromReview("approved, no changes requested"); d == nil || d.Permission != "allow" {
		t.Errorf("approved phrase: want allow, got %+v", d)
	}
	approvedWithNotes := `# Code Review — Approved with Notes

Code review completed — the changes are approved. The notes below are non-blocking guidance, not a request for another revision.

## foo.go

Carry this into the next edit.`
	if d, _ := decisionFromReview(approvedWithNotes); d == nil || d.Permission != "allow" ||
		!strings.Contains(d.AdditionalContext, "Carry this into the next edit.") {
		t.Errorf("approved with notes: want allow carrying context, got %+v", d)
	}
	d, _ := decisionFromReview("fix the error handling\nand rename the var")
	if d == nil || d.Permission != "deny" {
		t.Fatalf("feedback: want deny, got %+v", d)
	}
	if !strings.Contains(d.Reason, "fix the error handling") {
		t.Errorf("deny reason should carry the feedback, got %q", d.Reason)
	}
}

// A gate that can't review must deny, never silently let the edit through.
func TestGateReviewGateFailsClosed(t *testing.T) {
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not available: %v", err)
	}

	// git and nothing else, so Plannotator is the only thing missing.
	gitOnly := t.TempDir()
	if err := os.Symlink(gitBin, filepath.Join(gitOnly, "git")); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		env     map[string]string
		wantMsg string
	}{
		{"no temp dir", map[string]string{"TMPDIR": filepath.Join(t.TempDir(), "absent")}, "staging repo"},
		{"no git", map[string]string{"PATH": ""}, "stage the edit"},
		{"no plannotator", map[string]string{"PATH": gitOnly}, "plannotator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// HOME is a fresh temp dir, so the ~/.local/bin plannotator misses too.
			t.Setenv("HOME", t.TempDir())
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(flagPath("live"), nil, 0o644); err != nil {
				t.Fatal(err)
			}

			work := t.TempDir()
			d := gateReviewGate(context.Background(), &Event{
				HookEventName: "PreToolUse",
				SessionID:     "live",
				CWD:           work,
				ToolName:      "Write",
				ToolInput: mustJSON(t, map[string]any{
					"file_path": filepath.Join(work, "foo.go"),
					"content":   "package main\nfunc f() {}\n",
				}),
			})
			if d == nil || d.Permission != "deny" {
				t.Fatalf("broken gate: want deny, got %+v", d)
			}
			if !strings.Contains(d.Reason, "NOT reviewed") || !strings.Contains(d.Reason, tc.wantMsg) {
				t.Errorf("deny reason should say the edit was not reviewed and name the cause %q, got %q", tc.wantMsg, d.Reason)
			}
		})
	}
}

// The skip gauntlet must return nil (defer) WITHOUT opening Plannotator for
// changes that don't warrant review. A nil return proves it short-circuited
// before staging/shelling out.
func TestGateReviewGateDefers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	file := filepath.Join(work, "foo.go")
	if err := os.WriteFile(file, []byte("package main\nfunc f() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	writeEvent := func(session, content string) *Event {
		return &Event{
			HookEventName: "PreToolUse",
			SessionID:     session,
			CWD:           work,
			ToolName:      "Write",
			ToolInput:     mustJSON(t, map[string]any{"file_path": file, "content": content}),
		}
	}

	// Gate disabled for this session → defer even for a real change.
	if d := gateReviewGate(context.Background(), writeEvent("no-flag", "package main\nfunc f() { x := 1; _ = x }\n")); d != nil {
		t.Errorf("disabled session: want defer, got %+v", d)
	}

	// Enable the gate, then whitespace-only and comment-only changes defer.
	if err := os.WriteFile(flagPath("live"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if d := gateReviewGate(context.Background(), writeEvent("live", "package main\n\nfunc f() {}\n")); d != nil {
		t.Errorf("whitespace-only change: want defer, got %+v", d)
	}
	if d := gateReviewGate(context.Background(), writeEvent("live", "package main\n// added\nfunc f() {}\n")); d != nil {
		t.Errorf("comment-only change: want defer, got %+v", d)
	}
	if d := gateReviewGate(context.Background(), writeEvent("live", "package main\nfunc f() {}\n")); d != nil {
		t.Errorf("no-op change: want defer, got %+v", d)
	}

	// A tool that doesn't write files is not the gate's business.
	notAnEdit := writeEvent("live", "package main\nfunc f() { x := 1; _ = x }\n")
	notAnEdit.ToolName = "Bash"
	if d := gateReviewGate(context.Background(), notAnEdit); d != nil {
		t.Errorf("ungated tool: want defer, got %+v", d)
	}

	// A scratchpad path skips regardless of content.
	scratch := &Event{
		HookEventName: "PreToolUse",
		SessionID:     "live",
		CWD:           work,
		ToolName:      "Write",
		ToolInput: mustJSON(t, map[string]any{
			"file_path": "/tmp/claude-501/scratch/x.go",
			"content":   "package main\nfunc g() { y := 2; _ = y }\n",
		}),
	}
	if d := gateReviewGate(context.Background(), scratch); d != nil {
		t.Errorf("scratchpad path: want defer, got %+v", d)
	}
}

// --skip-tests leaves test files ungated but still reviews everything else.
func TestGateReviewGateSkipTests(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flagPath("live"), []byte("skip-tests\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	testFile := filepath.Join(work, "foo_test.go")
	if err := os.WriteFile(testFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ev := &Event{
		HookEventName: "PreToolUse",
		SessionID:     "live",
		CWD:           work,
		ToolName:      "Write",
		ToolInput: mustJSON(t, map[string]any{
			"file_path": testFile,
			"content":   "package main\nfunc TestX(t *testing.T) {}\n",
		}),
	}
	if d := gateReviewGate(context.Background(), ev); d != nil {
		t.Errorf("test file with skip-tests: want defer, got %+v", d)
	}

	// The same session still gates a non-test file (PATH is empty, so a gated
	// edit fails closed — proof it got past the skip gauntlet).
	src := filepath.Join(work, "foo.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev.ToolInput = mustJSON(t, map[string]any{
		"file_path": src,
		"content":   "package main\nfunc f() { x := 1; _ = x }\n",
	})
	if d := gateReviewGate(context.Background(), ev); d == nil || d.Permission != "deny" {
		t.Errorf("non-test file with skip-tests: want deny, got %+v", d)
	}
}

func TestGateFailsClosedForMalformedCodexPatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flagPath("codex-session"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	ev := &Event{
		host:          hostCodex,
		HookEventName: "PreToolUse",
		SessionID:     "codex-session",
		CWD:           work,
		ToolName:      "apply_patch",
		ToolInput: mustJSON(t, map[string]any{
			"command": "*** Begin Patch\n*** Add File: broken.go\n+package broken\n",
		}),
	}
	d := gateReviewGate(context.Background(), ev)
	if d == nil || d.Permission != "deny" ||
		!strings.Contains(d.Reason, "could not preview") {
		t.Fatalf("malformed Codex patch should fail closed, got %+v", d)
	}
}

func TestCursorDeleteReachesGate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flagPath("cursor-session"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	path := filepath.Join(work, "delete.go")
	if err := os.WriteFile(path, []byte("package delete\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := &Event{
		host:          hostCursor,
		HookEventName: "PreToolUse",
		SessionID:     "cursor-session",
		CWD:           work,
		ToolName:      "Delete",
		ToolInput:     mustJSON(t, map[string]any{"path": path}),
	}
	d := gateReviewGate(context.Background(), ev)
	if d == nil || d.Permission != "deny" ||
		!strings.Contains(d.Reason, "stage the edit") {
		t.Fatalf("Cursor Delete should reach the fail-closed gate, got %+v", d)
	}
}

func TestRespondDecision(t *testing.T) {
	out, err := respondDecision(hostClaude, &Decision{Permission: "deny", Reason: "line one\nline two"})
	if err != nil {
		t.Fatal(err)
	}
	// Claude Code parses hook stdout as JSON; multiline reasons must survive.
	for _, want := range []string{`"hookEventName":"PreToolUse"`, `"permissionDecision":"deny"`, `line one\nline two`} {
		if !strings.Contains(out, want) {
			t.Errorf("envelope missing %q: %s", want, out)
		}
	}

	cursorOut, err := respondDecision(hostCursor, &Decision{Permission: "deny", Reason: "fix this"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"permission":"deny"`,
		`"agent_message":"fix this"`,
		`"user_message":"fix this"`,
	} {
		if !strings.Contains(cursorOut, want) {
			t.Errorf("Cursor envelope missing %q: %s", want, cursorOut)
		}
	}

	rewritten, err := respondDecision(hostCursor, &Decision{
		Permission:   "allow",
		UpdatedInput: map[string]any{"command": "rewritten"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rewritten, `"updated_input":{"command":"rewritten"}`) {
		t.Errorf("Cursor rewrite envelope is malformed: %s", rewritten)
	}
}
