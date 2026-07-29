package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The gate is per-session: an enabled session has a flag file here, named for
// the session id, whose body holds space-separated options.
const sessionsDirName = "plannotator-review-gate.sessions"

// staleFlagAge: flags untouched this long are swept on the next toggle. Active
// sessions refresh their flag mtime on every gated edit, so only dead ones die.
const staleFlagAge = 7 * 24 * time.Hour

func claudeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".claude")
}

// sessionsDir holds ONE kind of file: a per-session gate flag named for its
// session id. Nothing else belongs here — pruneStaleFlags reaps every file in
// it on an mtime rule, which would silently eat unrelated state.
func sessionsDir() string {
	return filepath.Join(claudeDir(), sessionsDirName)
}

func flagPath(sessionID string) string {
	return filepath.Join(sessionsDir(), sessionID)
}

// gateOpts returns a session's enabled options and whether the gate is on.
func gateOpts(sessionID string) (opts map[string]bool, enabled bool) {
	if sessionID == "" {
		return nil, false
	}
	body, err := os.ReadFile(flagPath(sessionID))
	if err != nil {
		return nil, false
	}
	opts = map[string]bool{}
	for f := range strings.FieldsSeq(string(body)) {
		opts[f] = true
	}
	return opts, true
}

// reviewGateCommand is the per-session toggle: on|off|toggle|status, optionally
// --skip-tests. The session id comes from CLAUDE_CODE_SESSION_ID, so a bare
// shell invocation errors instead of guessing which session to act on.
func reviewGateCommand(args []string) {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	if verb == "help" || verb == "-h" || verb == "--help" {
		printUsage()
		return
	}

	sessionID := os.Getenv("CLAUDE_CODE_SESSION_ID")
	if sessionID == "" {
		fmt.Fprintln(os.Stderr, "review gate: no CLAUDE_CODE_SESSION_ID — run this "+
			"inside a Claude Code session (the gate is toggled per session).")
		os.Exit(64)
	}

	skipTests := false
	for _, a := range args[1:] {
		if a == "--skip-tests" {
			skipTests = true
		}
	}

	flag := flagPath(sessionID)
	pruneStaleFlags()
	_, enabled := gateOpts(sessionID)

	switch {
	case verb == "on" || (verb == "toggle" && !enabled):
		body := ""
		if skipTests {
			body = "skip-tests\n"
		}
		if err := os.MkdirAll(sessionsDir(), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "review gate: %v\n", err)
			os.Exit(1)
		}
		if err := os.WriteFile(flag, []byte(body), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "review gate: %v\n", err)
			os.Exit(1)
		}
		extra := ""
		if skipTests {
			extra = " (skipping test files)"
		}
		fmt.Printf("review gate: enabled for this session%s — every gated "+
			"Edit/Write opens in Plannotator's review UI\n", extra)
		updateReviewGateSidebar(true)
	case verb == "off" || verb == "toggle":
		_ = os.Remove(flag)
		fmt.Println("review gate: disabled for this session — edits follow the " +
			"normal permission flow")
		updateReviewGateSidebar(false)
	case verb == "status":
		opts, on := gateOpts(sessionID)
		if !on {
			fmt.Println("review gate: disabled for this session")
			return
		}
		extra := ""
		if opts["skip-tests"] {
			extra = " (skipping test files)"
		}
		fmt.Printf("review gate: enabled for this session%s\n", extra)
	default:
		printUsage()
		os.Exit(64)
	}
}

func printUsage() {
	fmt.Println("plannotator-review-gate — review every Claude Code edit before it lands")
	fmt.Println()
	fmt.Println("usage:")
	fmt.Println("  plannotator-review-gate on [--skip-tests]   enable for this session")
	fmt.Println("  plannotator-review-gate off                 disable for this session")
	fmt.Println("  plannotator-review-gate toggle              flip it")
	fmt.Println("  plannotator-review-gate status              is it on?")
	fmt.Println("  plannotator-review-gate hook-config         print the settings.json wiring")
	fmt.Println("  plannotator-review-gate version            ")
	fmt.Println()
	fmt.Println("With no arguments it runs as a Claude Code PreToolUse hook, reading the")
	fmt.Println("event from stdin. The toggle is per session, keyed off CLAUDE_CODE_SESSION_ID.")
}

// pruneStaleFlags removes flags left by sessions that exited without disabling
// the gate. Best-effort — a cleanup error never breaks a toggle.
func pruneStaleFlags() {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleFlagAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(sessionsDir(), e.Name()))
		}
	}
}
