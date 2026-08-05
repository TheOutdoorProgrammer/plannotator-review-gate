package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Tags what the gate posts. Useful over the API, useless when reading the
// verdict: the exported feedback carries no source — see stripNotes.
const annotationSource = "plannotator-review-gate"

// Long enough for a cold Plannotator start, short enough that a review which
// never registers doesn't hold the edit hostage.
const (
	sessionWait = 20 * time.Second
	pollEvery   = 150 * time.Millisecond
)

// One entry in Plannotator's session registry (<data dir>/sessions/<pid>.json).
// Matched on Project, not the child's pid: `plannotator` may be a wrapper that
// execs the real binary, while the staging repo's name is unique per edit.
type plannotatorSession struct {
	Port    int    `json:"port"`
	URL     string `json:"url"`
	Mode    string `json:"mode"`
	Project string `json:"project"`
}

// plannotatorDataDir mirrors Plannotator's own resolution order
// (packages/shared/data-dir.ts): explicit override, existing legacy dir, XDG,
// legacy default.
func plannotatorDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	if dir := os.Getenv("PLANNOTATOR_DATA_DIR"); dir != "" {
		if after, ok := strings.CutPrefix(dir, "~"); ok {
			dir = filepath.Join(home, after)
		}
		return dir
	}
	legacy := filepath.Join(home, ".plannotator")
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "plannotator")
	}
	return legacy
}

// findReviewSession waits for the review of project to register itself. Not
// found is a normal outcome: narration is optional, the review still happens.
func findReviewSession(ctx context.Context, project string) (string, bool) {
	dir := filepath.Join(plannotatorDataDir(), "sessions")
	deadline := time.Now().Add(sessionWait)
	for {
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				body, err := os.ReadFile(filepath.Join(dir, e.Name()))
				if err != nil {
					continue
				}
				var s plannotatorSession
				if json.Unmarshal(body, &s) != nil {
					continue
				}
				if s.Mode == "review" && s.Project == project && s.URL != "" {
					return strings.TrimSuffix(s.URL, "/"), true
				}
			}
		}
		if time.Now().After(deadline) {
			return "", false
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-time.After(pollEvery):
		}
	}
}

// waitForDiffReady blocks until the review reports an origin. Without one the
// UI never subscribes to external annotations and silently drops every post.
func waitForDiffReady(ctx context.Context, baseURL string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(sessionWait)
	for {
		if origin, ok := fetchOrigin(ctx, client, baseURL); ok && origin != "" {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(pollEvery):
		}
	}
}

func fetchOrigin(ctx context.Context, client *http.Client, baseURL string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/diff", nil)
	if err != nil {
		return "", false
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var payload struct {
		Origin string `json:"origin"`
	}
	if json.NewDecoder(resp.Body).Decode(&payload) != nil {
		return "", false
	}
	return payload.Origin, true
}

// externalAnnotation is the POST body Plannotator accepts.
type externalAnnotation struct {
	Source    string `json:"source"`
	Author    string `json:"author,omitempty"`
	Scope     string `json:"scope,omitempty"`
	FilePath  string `json:"filePath,omitempty"`
	LineStart int    `json:"lineStart,omitempty"`
	LineEnd   int    `json:"lineEnd,omitempty"`
	Type      string `json:"type,omitempty"`
	Text      string `json:"text"`
}

// postNotes sends the queued notes as review comments. rel must be the path as
// it appears in the staged diff, or a line annotation never pins to its line.
func postNotes(ctx context.Context, baseURL, rel string, ns []note) error {
	anns := make([]externalAnnotation, 0, len(ns))
	for _, n := range ns {
		a := externalAnnotation{
			Source: annotationSource,
			Author: "Claude",
			Type:   n.Type,
			Text:   n.Text,
		}
		if n.LineStart > 0 {
			a.Scope, a.FilePath, a.LineStart, a.LineEnd = "line", rel, n.LineStart, n.LineEnd
		} else {
			a.Scope = "general"
		}
		anns = append(anns, a)
	}
	body, err := json.Marshal(map[string]any{"annotations": anns})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/api/external-annotations", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("posting annotations: %s", resp.Status)
	}
	return nil
}

// Removes the gate's own narration, or the agent's explanation returns as the
// reviewer's and denies the edit it explained. Text is the only handle (the
// export emits no source); a note Joey edited stops matching — that made it his.
func stripNotes(feedback string, ns []note) string {
	lines := strings.Split(feedback, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if isNoteLine(line, ns) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// Compares on the suffix: the export may prefix a conventional label.
func isNoteLine(line string, ns []note) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for _, n := range ns {
		text := strings.TrimSpace(n.Text)
		if text != "" && (trimmed == text || strings.HasSuffix(trimmed, text)) {
			return true
		}
	}
	return false
}

// reviewerLeftContent reports whether anything the reviewer wrote survived
// stripping. Headings and banners are emitted whether or not a human said
// anything, so a body of only those means every comment was ours.
func reviewerLeftContent(feedback string) bool {
	for line := range strings.SplitSeq(feedback, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "",
			strings.HasPrefix(trimmed, "#"),
			strings.HasPrefix(trimmed, "**Diff:**"),
			strings.HasPrefix(trimmed, "```"):
			continue
		}
		return true
	}
	return false
}
