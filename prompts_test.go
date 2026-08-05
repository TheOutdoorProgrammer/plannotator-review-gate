package main

import (
	"os"
	"path/filepath"
	"strings"
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

func TestStripDeniedSuffix(t *testing.T) {
	feedback := "## pkg/foo.go\n\nrename this thing\n\n" + deniedSuffixAnchor +
		"\n\nInspect every finding against the actual code.\n\n" +
		"Do not change any code until we have discussed the verdicts."

	got := stripDeniedSuffix(feedback)
	if !strings.Contains(got, "rename this thing") {
		t.Error("the reviewer's own feedback was stripped")
	}
	// The suffix tells the agent not to change code, which is the opposite of
	// what the gate just told it to do.
	if strings.Contains(got, "Do not change any code") {
		t.Errorf("the triage boilerplate survived: %q", got)
	}

	// Feedback without the suffix is untouched.
	plain := "## pkg/foo.go\n\nrename this thing"
	if got := stripDeniedSuffix(plain); got != plain {
		t.Errorf("stripDeniedSuffix altered plain feedback: %q", got)
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
