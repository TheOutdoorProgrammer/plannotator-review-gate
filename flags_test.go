package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewGateCommandToggle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-1")

	if _, on := gateOpts("sess-1"); on {
		t.Fatal("gate should start disabled")
	}

	reviewGateCommand([]string{"on"})
	opts, on := gateOpts("sess-1")
	if !on || opts["skip-tests"] {
		t.Fatalf("after on: enabled=%v opts=%v", on, opts)
	}

	reviewGateCommand([]string{"off"})
	if _, on := gateOpts("sess-1"); on {
		t.Fatal("after off: gate should be disabled")
	}

	reviewGateCommand([]string{"on", "--skip-tests"})
	opts, on = gateOpts("sess-1")
	if !on || !opts["skip-tests"] {
		t.Fatalf("after on --skip-tests: enabled=%v opts=%v", on, opts)
	}

	reviewGateCommand([]string{"toggle"})
	if _, on := gateOpts("sess-1"); on {
		t.Fatal("toggle from enabled should disable")
	}

	reviewGateCommand([]string{"toggle"})
	if _, on := gateOpts("sess-1"); !on {
		t.Fatal("toggle from disabled should enable")
	}
}

// Sessions are independent: enabling the gate in one must not gate another.
func TestGateOptsIsPerSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_SESSION_ID", "sess-a")
	reviewGateCommand([]string{"on"})

	if _, on := gateOpts("sess-a"); !on {
		t.Error("sess-a should be enabled")
	}
	if _, on := gateOpts("sess-b"); on {
		t.Error("sess-b should be unaffected")
	}
	if _, on := gateOpts(""); on {
		t.Error("an empty session id must never read as enabled")
	}
}

func TestPruneStaleFlags(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	stale := flagPath("dead-session")
	fresh := flagPath("live-session")
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-staleFlagAge - time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	pruneStaleFlags()

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale flag should have been pruned")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh flag should survive: %v", err)
	}
}

// ensure sessionsDir is under the temp HOME, not the real ~/.claude
func TestSessionsDirHonorsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".claude", sessionsDirName)
	if got := sessionsDir(); got != want {
		t.Errorf("sessionsDir() = %q, want %q", got, want)
	}
}

func TestCurrentSessionIDAcrossHosts(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "claude")
	if got := currentSessionID(); got != "claude" {
		t.Fatalf("Claude session id = %q", got)
	}
	t.Setenv("CODEX_THREAD_ID", "codex")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if got := currentSessionID(); got != "codex" {
		t.Fatalf("Codex session id = %q", got)
	}
	t.Setenv("PLANNOTATOR_REVIEW_GATE_SESSION_ID", "cursor")
	if got := currentSessionID(); got != "cursor" {
		t.Fatalf("Cursor session id = %q", got)
	}
}
