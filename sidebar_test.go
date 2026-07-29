package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCmux puts an executable named cmux on PATH that exits with the given
// code, standing in for "cmux is installed" without needing the real thing.
func fakeCmux(t *testing.T, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexit " + string(rune('0'+exitCode)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "cmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestSidebarCachesMissingCmux(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", "")
	if err := os.MkdirAll(claudeDir(), 0o755); err != nil {
		t.Fatal(err)
	}

	updateReviewGateSidebar(true)

	body, err := os.ReadFile(noSidebarPath())
	if err != nil {
		t.Fatalf("expected the marker to be written when cmux is absent: %v", err)
	}
	// Whoever stumbles on this file should learn what it does from the file.
	for _, want := range []string{"Delete this file", "cmux"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("marker body should mention %q, got:\n%s", want, body)
		}
	}
}

// The regression that matters: cmux installed but this shell isn't a cmux
// session (plain terminal, ssh, CI). That's transient — caching it would kill
// the indicator inside cmux too.
func TestSidebarDoesNotCacheNonCmuxSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(claudeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeCmux(t, 1) // present, but `identify` fails

	updateReviewGateSidebar(true)

	if _, err := os.Stat(noSidebarPath()); err == nil {
		t.Error("must NOT cache: cmux is installed, this just isn't a cmux session")
	}
}

// An existing marker short-circuits before cmux is ever consulted.
func TestSidebarHonorsExistingMarker(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(claudeDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noSidebarPath(), []byte("opted out\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeCmux(t, 0)

	updateReviewGateSidebar(true)

	// Untouched: the opt-out is honored, not rewritten.
	body, err := os.ReadFile(noSidebarPath())
	if err != nil || string(body) != "opted out\n" {
		t.Errorf("marker should be left alone, got %q (err %v)", body, err)
	}
}

// The marker lives OUTSIDE sessionsDir, so pruneStaleFlags can never reap it.
// (A marker inside that directory was deleted after 7 days.)
func TestMarkerSurvivesFlagPruning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(noSidebarPath(), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-staleFlagAge - time.Hour)
	if err := os.Chtimes(noSidebarPath(), old, old); err != nil {
		t.Fatal(err)
	}

	pruneStaleFlags()

	if _, err := os.Stat(noSidebarPath()); err != nil {
		t.Errorf("marker must survive flag pruning: %v", err)
	}
	if dir := filepath.Dir(noSidebarPath()); dir == sessionsDir() {
		t.Error("marker must not live inside sessionsDir")
	}
}
