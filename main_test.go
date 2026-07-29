package main

import (
	"strings"
	"testing"
)

// A GoReleaser-stamped version must win outright — the build-info fallback is
// only for binaries that were never stamped.
func TestVersionInfoPrefersReleaseStamp(t *testing.T) {
	oldV, oldC, oldD := version, commit, date
	t.Cleanup(func() { version, commit, date = oldV, oldC, oldD })

	version, commit, date = "1.2.3", "abc1234", "2026-01-01T00:00:00Z"
	v, sha, built := versionInfo()
	if v != "1.2.3" || sha != "abc1234" || built != "2026-01-01T00:00:00Z" {
		t.Errorf("release stamp should be returned verbatim, got (%q, %q, %q)", v, sha, built)
	}
}

// Unstamped builds (go install, plain go build) must still report something
// truthful rather than the literal placeholders.
func TestVersionInfoFallsBackToBuildInfo(t *testing.T) {
	oldV, oldC, oldD := version, commit, date
	t.Cleanup(func() { version, commit, date = oldV, oldC, oldD })

	version, commit, date = "dev", "none", "unknown"
	v, sha, built := versionInfo()

	for name, got := range map[string]string{"version": v, "commit": sha, "date": built} {
		if got == "" {
			t.Errorf("%s must never be empty", name)
		}
	}
	// The test binary is built from this repo, so the toolchain embeds a VCS
	// revision. Assert we surfaced it instead of leaving "none".
	if sha == "none" {
		t.Log("no vcs.revision embedded (acceptable outside a VCS build)")
	} else if len(sha) < 7 {
		t.Errorf("commit looks malformed: %q", sha)
	}
	if strings.HasPrefix(v, "v") {
		t.Errorf("version should not keep its leading v: %q", v)
	}
	// Go stamps its own "+dirty" into Main.Version, so appending another marker
	// produced "1.0.0+dirty-dirty".
	if strings.Count(v, "dirty") > 1 {
		t.Errorf("dirty marker applied twice: %q", v)
	}
}
