package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Feedback as Plannotator exports it. Our notes carry no source marker here —
// indistinguishable from the reviewer's, which is why stripping exists.
const exportedFeedback = `# Code Review Feedback

**Diff:** all changes since main

## pkg/foo.go

### Line 42

why the nil check moved above the type switch

### Line 90

this rename is wrong, call it handleEvent
`

func TestStripNotesRemovesOnlyOurOwn(t *testing.T) {
	ns := []note{{Path: "/repo/pkg/foo.go", LineStart: 42, LineEnd: 42,
		Text: "why the nil check moved above the type switch"}}

	got := stripNotes(exportedFeedback, ns)
	if strings.Contains(got, "why the nil check moved") {
		t.Error("our own note survived stripping — it would come back as the reviewer's feedback")
	}
	if !strings.Contains(got, "this rename is wrong, call it handleEvent") {
		t.Error("stripping ate the reviewer's actual feedback")
	}
}

// A conventional label is prefixed onto the body in the export, so matching has
// to tolerate it.
func TestStripNotesHandlesConventionalPrefix(t *testing.T) {
	ns := []note{{Text: "the ordering here is load-bearing"}}
	got := stripNotes("## a.go\n\nnote: the ordering here is load-bearing\n", ns)
	if strings.Contains(got, "load-bearing") {
		t.Errorf("prefixed note survived: %q", got)
	}
}

// If Joey edits one of our notes he has adopted it as his own feedback, so it
// must NOT be stripped.
func TestStripNotesLeavesAnEditedNote(t *testing.T) {
	ns := []note{{Text: "why the nil check moved"}}
	edited := "## a.go\n\nwhy the nil check moved — and it belongs in the caller instead\n"
	if got := stripNotes(edited, ns); !strings.Contains(got, "belongs in the caller") {
		t.Errorf("an edited note was stripped: %q", got)
	}
}

func TestReviewerLeftContent(t *testing.T) {
	cases := []struct {
		name     string
		feedback string
		want     bool
	}{
		{"structure only", "# Code Review Feedback\n\n**Diff:** since main\n\n## pkg/foo.go\n\n### Line 42\n\n", false},
		{"empty", "", false},
		{"real comment", "# Code Review Feedback\n\n## pkg/foo.go\n\nrename this\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reviewerLeftContent(tc.feedback); got != tc.want {
				t.Errorf("reviewerLeftContent = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerdictFromReview(t *testing.T) {
	ns := []note{{Text: "why the nil check moved above the type switch"}}

	// Only our notes in the review: not an approval, and not the reviewer's
	// feedback either.
	d, dismissed := verdictFromReview(exportedFeedbackOursOnly(), ns, true)
	if dismissed || d == nil || d.Permission != "deny" {
		t.Fatalf("ours only: want deny, got (%+v, %v)", d, dismissed)
	}
	if !strings.Contains(d.Reason, "without leaving any feedback") {
		t.Errorf("ours only: reason should say the reviewer left nothing, got %q", d.Reason)
	}

	// Narration must not poison an approval.
	d, _ = verdictFromReview("# Code Review\n\nCode review completed — no changes requested.", ns, true)
	if d == nil || d.Permission != "allow" {
		t.Fatalf("approved with notes posted: want allow, got %+v", d)
	}

	// Real feedback denies, carrying his words and not ours.
	d, _ = verdictFromReview(exportedFeedback, ns, true)
	if d == nil || d.Permission != "deny" {
		t.Fatalf("real feedback: want deny, got %+v", d)
	}
	if strings.Contains(d.Reason, "why the nil check moved") {
		t.Error("deny reason handed our own note back to us")
	}
	if !strings.Contains(d.Reason, "this rename is wrong") {
		t.Error("deny reason lost the reviewer's feedback")
	}

	// A dismissal is still a dismissal.
	if d, dismissed := verdictFromReview(reviewClosedMarker, ns, true); d != nil || !dismissed {
		t.Errorf("closed marker: want (nil, true), got (%+v, %v)", d, dismissed)
	}

	// Nothing posted: identical to the pre-notes behaviour.
	if d, _ := verdictFromReview(exportedFeedback, nil, false); d == nil || d.Permission != "deny" {
		t.Errorf("nothing posted: want deny, got %+v", d)
	}
}

func exportedFeedbackOursOnly() string {
	return "# Code Review Feedback\n\n**Diff:** all changes since main\n\n" +
		"## pkg/foo.go\n\n### Line 42\n\nwhy the nil check moved above the type switch\n"
}

// Must find the review the gate opened, not one already on screen.
func TestFindReviewSessionPicksTheMatchingProject(t *testing.T) {
	data := t.TempDir()
	t.Setenv("PLANNOTATOR_DATA_DIR", data)
	sessions := filepath.Join(data, "sessions")
	if err := os.MkdirAll(sessions, 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(pid int, s plannotatorSession) {
		body, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(sessions, string(rune('0'+pid))+".json")
		if err := os.WriteFile(name, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(1, plannotatorSession{Port: 111, URL: "http://localhost:111/", Mode: "review", Project: "some-other-repo"})
	write(2, plannotatorSession{Port: 222, URL: "http://localhost:222", Mode: "review", Project: "review-gate-99"})
	write(3, plannotatorSession{Port: 333, URL: "http://localhost:333", Mode: "plan", Project: "review-gate-99"})

	got, ok := findReviewSession(context.Background(), "review-gate-99")
	if !ok {
		t.Fatal("did not find the matching review session")
	}
	// The trailing slash is trimmed so callers can append /api/... safely.
	if got != "http://localhost:222" {
		t.Errorf("base URL = %q, want http://localhost:222", got)
	}
}

func TestFindReviewSessionGivesUpWhenCancelled(t *testing.T) {
	t.Setenv("PLANNOTATOR_DATA_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, ok := findReviewSession(ctx, "review-gate-1"); ok {
		t.Error("found a session in an empty registry")
	}
}

func TestPlannotatorDataDirPrefersExplicitOverride(t *testing.T) {
	t.Setenv("PLANNOTATOR_DATA_DIR", "/custom/plannotator")
	if got := plannotatorDataDir(); got != "/custom/plannotator" {
		t.Errorf("data dir = %q, want the override", got)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PLANNOTATOR_DATA_DIR", "~/relocated")
	if got := plannotatorDataDir(); got != filepath.Join(home, "relocated") {
		t.Errorf("data dir = %q, want ~ expanded under %q", got, home)
	}
}
