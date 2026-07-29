// plannotator-review-gate is a Claude Code PreToolUse hook that puts every
// proposed file edit in front of you in Plannotator's code-review UI before it
// lands on disk. Approve and the edit applies; leave line comments and the edit
// is denied with your feedback handed back to the model, which revises and
// proposes again.
//
// It is opt-in per session (`plannotator-review-gate on`), so one terminal can
// review every edit while another works unimpeded.
//
// Run `plannotator-review-gate hook-config` for the settings.json wiring.
// See README.md for setup.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
)

// Injected by GoReleaser at build time (-ldflags -X main.version=...). Only
// release builds carry them — see versionInfo for everything else.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// versionInfo reports what this binary actually is. Release builds are stamped
// by GoReleaser; `go install pkg@vX.Y.Z` and local `go build` get no ldflags at
// all, so fall back to the module version and VCS stamps the toolchain embeds.
func versionInfo() (v, sha, built string) {
	v, sha, built = version, commit, date
	if v != "dev" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	// go install records the resolved module version; a local build says "(devel)".
	if mv := bi.Main.Version; mv != "" && mv != "(devel)" {
		v = strings.TrimPrefix(mv, "v")
	}
	dirty := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			sha = s.Value
		case "vcs.time":
			built = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	// Go may already have stamped its own "+dirty" into Main.Version; don't
	// append a second marker on top of it.
	if dirty && !strings.Contains(v, "dirty") {
		v += "-dirty"
	}
	return v, sha, built
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "on", "off", "toggle", "status", "help", "-h", "--help":
			reviewGateCommand(os.Args[1:])
		case "hook-config":
			printHookConfig()
		case "version", "--version":
			v, sha, built := versionInfo()
			fmt.Printf("plannotator-review-gate %s (commit %s, built %s)\n", v, sha, built)
		default:
			fmt.Fprintf(os.Stderr, "plannotator-review-gate: unknown command %q\n\n", os.Args[1])
			reviewGateCommand([]string{"help"})
		}
		return
	}
	hookMode()
}

// hookMode is the no-args path: event JSON on stdin, verdict on stdout.
//
// Failure handling is asymmetric on purpose. A gate that can't review fails
// CLOSED (see gateBroken); a panic is a bug here rather than a review failure,
// so it exits 0 and lets the edit follow the normal permission flow — unless
// the session had the gate ON, where nothing may land unseen.
func hookMode() {
	var ev Event
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "plannotator-review-gate: %v\n", r)
			if _, enabled := gateOpts(ev.SessionID); enabled {
				emit(gateBroken(fmt.Sprintf("the gate crashed (%v)", r)))
			}
			os.Exit(0)
		}
	}()

	if err := json.NewDecoder(os.Stdin).Decode(&ev); err != nil {
		fmt.Fprintf(os.Stderr, "plannotator-review-gate: decoding event: %v\n", err)
		return
	}
	if ev.HookEventName != "PreToolUse" {
		return
	}
	ctx, cancel := gateContext()
	defer cancel()
	emit(gateReviewGate(ctx, &ev))
}

// emit prints the PreToolUse verdict, if any. A nil Decision means defer: print
// nothing and let the normal permission flow decide.
func emit(d *Decision) {
	if d == nil {
		return
	}
	out, err := respondDecision(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plannotator-review-gate: %v\n", err)
		return
	}
	fmt.Println(out)
}
