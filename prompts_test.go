package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writePlannotatorConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PLANNOTATOR_DATA_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsApprovedUsesTheShippedDefault(t *testing.T) {
	t.Setenv("PLANNOTATOR_DATA_DIR", t.TempDir())
	if !isApproved("# Code Review\n\nCode review completed — no changes requested.") {
		t.Error("the default approved prompt was not recognised")
	}
	if isApproved("# Code Review Feedback\n\nrename this thing") {
		t.Error("feedback was read as an approval")
	}
}

// A customised prompt must still read as approval, or the gate denies every
// approved edit.
func TestIsApprovedHonoursAConfiguredPrompt(t *testing.T) {
	writePlannotatorConfig(t, `{"prompts":{"review":{"approved":"LGTM, ship it"}}}`)
	if !isApproved("LGTM, ship it") {
		t.Error("configured approved prompt was not recognised")
	}
	// The default keeps working alongside an override.
	if !isApproved("Code review completed — no changes requested.") {
		t.Error("the default stopped being recognised once a config existed")
	}
}

func TestIsApprovedHonoursARuntimePrompt(t *testing.T) {
	writePlannotatorConfig(t,
		`{"prompts":{"review":{"runtimes":{"claude-code":{"approved":"approved by claude-code"}}}}}`)
	if !isApproved("approved by claude-code") {
		t.Error("runtime-specific approved prompt was not recognised")
	}
}

// An empty or malformed override must not become a marker that matches
// everything — that would approve edits the reviewer rejected.
func TestIsApprovedIgnoresEmptyAndBrokenConfig(t *testing.T) {
	cases := []struct {
		name, body string
	}{
		{"empty override", `{"prompts":{"review":{"approved":"   "}}}`},
		{"not json", `{{{`},
		{"unrelated shape", `{"prompts":{"review":[]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writePlannotatorConfig(t, tc.body)
			if isApproved("# Code Review Feedback\n\nthis is wrong") {
				t.Error("feedback was read as an approval")
			}
		})
	}
}
