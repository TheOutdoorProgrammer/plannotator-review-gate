package main

import (
	"path/filepath"
	"testing"
)

func TestParseLineSpec(t *testing.T) {
	cases := []struct {
		spec         string
		start, end   int
		wantErr      bool
		whyItMatters string
	}{
		{spec: "", start: 0, end: 0, whyItMatters: "no --line posts as a general note, not line 0"},
		{spec: "42", start: 42, end: 42},
		{spec: "42-44", start: 42, end: 44},
		{spec: " 42 - 44 ", start: 42, end: 44, whyItMatters: "shell quoting leaves spaces"},
		{spec: "0", wantErr: true, whyItMatters: "files start at line 1"},
		{spec: "-5", wantErr: true},
		{spec: "44-42", wantErr: true, whyItMatters: "a backwards range is a typo, not a range"},
		{spec: "abc", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.spec, func(t *testing.T) {
			start, end, err := parseLineSpec(tc.spec)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseLineSpec(%q): want error, got (%d, %d)", tc.spec, start, end)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseLineSpec(%q): %v", tc.spec, err)
			}
			if start != tc.start || end != tc.end {
				t.Errorf("parseLineSpec(%q) = (%d, %d), want (%d, %d)", tc.spec, start, end, tc.start, tc.end)
			}
		})
	}
}

func TestNotesQueueRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	session := "sess-1"

	if got := loadNotes(session); len(got) != 0 {
		t.Fatalf("empty queue: got %d notes", len(got))
	}

	want := []note{
		{Path: "/repo/a.go", LineStart: 10, LineEnd: 12, Type: "comment", Text: "why the nil check moved"},
		{Path: "/repo/b.go", Type: "concern", Text: "not sure this handles the empty case"},
	}
	if err := saveNotes(session, want); err != nil {
		t.Fatalf("saveNotes: %v", err)
	}
	got := loadNotes(session)
	if len(got) != len(want) {
		t.Fatalf("round trip: got %d notes, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("note %d = %+v, want %+v", i, got[i], want[i])
		}
	}

	// Saving an empty queue removes the file rather than leaving "null" behind.
	if err := saveNotes(session, nil); err != nil {
		t.Fatalf("saveNotes(nil): %v", err)
	}
	if got := loadNotes(session); len(got) != 0 {
		t.Errorf("cleared queue: got %d notes", len(got))
	}
}

// A queue is per session: one session's notes must never surface in another's
// review.
func TestNotesAreScopedPerSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveNotes("sess-a", []note{{Path: "/repo/a.go", Text: "mine"}}); err != nil {
		t.Fatalf("saveNotes: %v", err)
	}
	if got := loadNotes("sess-b"); len(got) != 0 {
		t.Errorf("sess-b sees %d of sess-a's notes", len(got))
	}
}

func TestNotesFor(t *testing.T) {
	all := []note{
		{Path: "/repo/a.go", Text: "first about a"},
		{Path: "/repo/b.go", Text: "about b"},
		{Path: "/repo/a.go", Text: "second about a"},
	}
	mine, rest := notesFor(all, "/repo/a.go")
	if len(mine) != 2 || mine[0].Text != "first about a" || mine[1].Text != "second about a" {
		t.Errorf("matched = %+v, want both a.go notes in order", mine)
	}
	// The unmatched note stays queued for the edit it was written about.
	if len(rest) != 1 || rest[0].Path != "/repo/b.go" {
		t.Errorf("rest = %+v, want just the b.go note", rest)
	}
}

func TestStagedRelPath(t *testing.T) {
	cases := []struct {
		name, path, cwd, want string
	}{
		{"under cwd", "/repo/pkg/foo.go", "/repo", filepath.Join("pkg", "foo.go")},
		{"at cwd root", "/repo/foo.go", "/repo", "foo.go"},
		// Outside the session cwd there is no sensible relative path, and the
		// staged repo only holds the file itself.
		{"outside cwd", "/elsewhere/foo.go", "/repo", "foo.go"},
		{"no cwd", "/repo/pkg/foo.go", "", "foo.go"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := stagedRelPath(tc.path, tc.cwd); got != tc.want {
				t.Errorf("stagedRelPath(%q, %q) = %q, want %q", tc.path, tc.cwd, got, tc.want)
			}
		})
	}
}
