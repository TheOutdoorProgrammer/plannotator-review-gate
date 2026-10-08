package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Queued explanations, posted by the gate when the edit they describe is
// reviewed. The agent can't post them itself: the hook blocks the edit tool call
// for the review's whole life, so it is never running while the review is open.
const notesDirName = "plannotator-review-gate.notes"

// noteTypes are the annotation types Plannotator renders. "concern" is the one
// worth reaching for when the agent wants a decision challenged.
var noteTypes = []string{"comment", "suggestion", "concern"}

// Caps on a note that isn't pinned to the code it explains. Notes are queued
// before the edit exists, so pinning means working out the post-edit line
// number — and an agent that skips that work parks everything on line 1.
const (
	maxLooseNoteLines = 3
	maxLooseNoteChars = 400
)

const splitHint = "Split it: one note per line it explains (--line N), and keep a " +
	"review-level note to what the whole change does."

// A note is one queued explanation. LineStart of 0 means the note is not tied
// to a line and is posted as a review-level (general) annotation.
type note struct {
	Path      string `json:"path"`
	LineStart int    `json:"line_start,omitempty"`
	LineEnd   int    `json:"line_end,omitempty"`
	Type      string `json:"type,omitempty"`
	Text      string `json:"text"`
}

func notesDir() string {
	return filepath.Join(claudeDir(), notesDirName)
}

func notesPath(sessionID string) string {
	return filepath.Join(notesDir(), sessionID+".json")
}

// loadNotes returns a session's queued notes. A missing or corrupt queue reads
// as empty: narration is a nicety, and losing it must never fail an edit.
func loadNotes(sessionID string) []note {
	body, err := os.ReadFile(notesPath(sessionID))
	if err != nil {
		return nil
	}
	var ns []note
	if json.Unmarshal(body, &ns) != nil {
		return nil
	}
	return ns
}

func saveNotes(sessionID string, ns []note) error {
	if err := os.MkdirAll(notesDir(), 0o755); err != nil {
		return err
	}
	if len(ns) == 0 {
		err := os.Remove(notesPath(sessionID))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	body, err := json.MarshalIndent(ns, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(notesPath(sessionID), append(body, '\n'), 0o644)
}

// notesFor splits a session's queue into the notes about path and the rest.
// The gate reads with this and only drops the matched ones once they are posted,
// so a failed post leaves them queued for the next edit to that file.
func notesFor(ns []note, path string) (matched, rest []note) {
	for _, n := range ns {
		if n.Path == path {
			matched = append(matched, n)
		} else {
			rest = append(rest, n)
		}
	}
	return matched, rest
}

// noteCommand queues one note for the current session:
//
//	note <file> [--line N | --line N-M] [--type comment|suggestion|concern] "text"
func noteCommand(args []string) {
	sessionID := currentSessionID()
	if sessionID == "" {
		fmt.Fprintln(os.Stderr, "review gate: no supported agent session id; run this "+
			"inside Claude Code, Codex, or Cursor (notes are queued per session).")
		os.Exit(64)
	}

	var positional []string
	noteType := "comment"
	lineSpec := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--line", "--type":
			flag := args[i]
			if i+1 >= len(args) {
				noteUsageError(fmt.Sprintf("%s needs a value", flag))
			}
			i++
			if flag == "--line" {
				lineSpec = args[i]
			} else {
				noteType = args[i]
			}
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) != 2 {
		noteUsageError("expected a file and a note")
	}
	if !contains(noteTypes, noteType) {
		noteUsageError(fmt.Sprintf("unknown --type %q (want %s)", noteType, strings.Join(noteTypes, ", ")))
	}

	start, end, err := parseLineSpec(lineSpec)
	if err != nil {
		noteUsageError(err.Error())
	}

	path, err := filepath.Abs(positional[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "review gate: %v\n", err)
		os.Exit(1)
	}
	text := strings.TrimSpace(positional[1])
	if text == "" {
		noteUsageError("the note text is empty")
	}

	pruneStaleNotes()
	queued := loadNotes(sessionID)
	n := note{Path: filepath.Clean(path), LineStart: start, LineEnd: end, Type: noteType, Text: text}
	if problem := placementProblem(n, queued); problem != "" {
		noteUsageError(problem)
	}
	if err := saveNotes(sessionID, append(queued, n)); err != nil {
		fmt.Fprintf(os.Stderr, "review gate: %v\n", err)
		os.Exit(1)
	}

	where := "the review"
	if start > 0 {
		where = fmt.Sprintf("line %s", lineSpec)
	}
	fmt.Printf("review gate: note queued on %s for %s — it posts when that edit is reviewed\n",
		where, filepath.Base(path))
}

// looselyPlaced reports whether a note is unpinned: no --line, or a range from
// line 1 — the stand-in agents use for "somewhere in this file".
func looselyPlaced(n note) bool {
	return n.LineStart == 0 || n.LineStart == 1
}

// placementProblem returns why a note can't be queued as placed, or "" if it is
// fine. Only loosely-placed notes are policed: pinning already takes the work
// this is trying to force, so a long note on a real line is left alone.
func placementProblem(n note, queued []note) string {
	if !looselyPlaced(n) {
		return ""
	}
	const unpinned = "isn't pinned to the code it explains (no --line, or --line 1)"
	if lines := strings.Count(n.Text, "\n") + 1; lines > maxLooseNoteLines {
		return fmt.Sprintf("this note is %d lines and %s. %s", lines, unpinned, splitHint)
	}
	if chars := len([]rune(n.Text)); chars > maxLooseNoteChars {
		return fmt.Sprintf("this note is %d characters (the cap is %d) and %s. %s",
			chars, maxLooseNoteChars, unpinned, splitHint)
	}
	for _, q := range queued {
		if q.Path == n.Path && looselyPlaced(q) {
			return fmt.Sprintf("%s already has an unpinned note queued (%q). Pin this one to the "+
				"line it explains (--line N), or fold it into that one — notes piled on the same "+
				"spot read as one wall of text.", filepath.Base(n.Path), truncate(q.Text, 60))
		}
	}
	return ""
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// parseLineSpec reads "N" or "N-M". An empty spec means no line, which posts as
// a review-level annotation rather than silently pinning to line 0.
func parseLineSpec(spec string) (start, end int, err error) {
	if spec == "" {
		return 0, 0, nil
	}
	first, last, split := strings.Cut(spec, "-")
	start, err = strconv.Atoi(strings.TrimSpace(first))
	if err != nil || start < 1 {
		return 0, 0, fmt.Errorf("--line %q is not a line number", spec)
	}
	if !split {
		return start, start, nil
	}
	end, err = strconv.Atoi(strings.TrimSpace(last))
	if err != nil || end < start {
		return 0, 0, fmt.Errorf("--line %q is not a line range", spec)
	}
	return start, end, nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func noteUsageError(detail string) {
	fmt.Fprintf(os.Stderr, "review gate: %s\n\n", detail)
	fmt.Fprintln(os.Stderr, "usage: plannotator-review-gate note <file> [--line N | --line N-M] "+
		"[--type comment|suggestion|concern] \"why this change looks like this\"")
	os.Exit(64)
}

// pruneStaleNotes drops queues left by sessions that ended without their notes
// ever being posted — an edit that never happened, or a gate that was off.
func pruneStaleNotes() {
	entries, err := os.ReadDir(notesDir())
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-staleFlagAge)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(notesDir(), e.Name()))
		}
	}
}
