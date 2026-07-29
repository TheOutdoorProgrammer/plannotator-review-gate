package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// A change is a proposed file edit: resolved path, current on-disk content
// (before, empty when the file is new), the content the tool would write
// (after), and whether the file already exists.
type change struct {
	path   string
	before string
	after  string
	exists bool
}

// proposedChange computes the (before, after) the tool would produce, or nil
// when the gate should stay out of the way (unreadable input, no file_path, or
// an Edit whose old_string doesn't match — the tool surfaces its own error).
func proposedChange(toolName string, toolInput json.RawMessage, cwd string) *change {
	var in struct {
		FilePath   string `json:"file_path"`
		Content    string `json:"content"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
		Edits      []struct {
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(toolInput, &in); err != nil || in.FilePath == "" {
		return nil
	}

	path := in.FilePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)

	before, exists := "", false
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		before, exists = string(b), true
	}

	switch toolName {
	case "Write":
		return &change{path: path, before: before, after: in.Content, exists: exists}
	case "Edit":
		if !exists {
			return nil
		}
		after, ok := applyEdit(before, in.OldString, in.NewString, in.ReplaceAll)
		if !ok {
			return nil
		}
		return &change{path: path, before: before, after: after, exists: exists}
	case "MultiEdit":
		if !exists {
			return nil
		}
		after := before
		for _, e := range in.Edits {
			var ok bool
			after, ok = applyEdit(after, e.OldString, e.NewString, e.ReplaceAll)
			if !ok {
				return nil
			}
		}
		return &change{path: path, before: before, after: after, exists: exists}
	}
	return nil
}

// applyEdit mirrors the Edit tool: a non-empty old_string must be present;
// replaceAll swaps every occurrence, else the first. ok is false when
// old_string is empty or absent (no edit to preview).
func applyEdit(src, old, replacement string, replaceAll bool) (string, bool) {
	if old == "" || !strings.Contains(src, old) {
		return "", false
	}
	if replaceAll {
		return strings.ReplaceAll(src, old, replacement), true
	}
	return strings.Replace(src, old, replacement, 1), true
}
