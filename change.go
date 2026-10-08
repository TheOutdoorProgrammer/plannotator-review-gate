package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A change is a proposed file edit: resolved path, current on-disk content
// (before, empty when the file is new), the content the tool would write
// (after), and whether the file already exists.
type change struct {
	path    string
	before  string
	after   string
	exists  bool
	deleted bool
}

// proposedChanges computes the file set a tool call would produce. A malformed
// supported edit is an error so an enabled gate can fail closed.
func proposedChanges(toolName string, toolInput json.RawMessage, cwd string) ([]change, error) {
	var in struct {
		FilePath   string  `json:"file_path"`
		Path       string  `json:"path"`
		Content    *string `json:"content"`
		Contents   *string `json:"contents"`
		OldString  string  `json:"old_string"`
		NewString  string  `json:"new_string"`
		ReplaceAll bool    `json:"replace_all"`
		Patch      string  `json:"patch"`
		Command    string  `json:"command"`
		Edits      []struct {
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		} `json:"edits"`
	}
	if err := json.Unmarshal(toolInput, &in); err != nil {
		return nil, fmt.Errorf("decode tool input: %w", err)
	}
	patch := in.Patch
	if patch == "" && strings.HasPrefix(in.Command, "*** Begin Patch") {
		patch = in.Command
	}
	if toolName == "apply_patch" || toolName == "ApplyPatch" || patch != "" {
		if patch == "" {
			return nil, fmt.Errorf("%s input has no patch command", toolName)
		}
		return parseApplyPatch(patch, cwd)
	}

	pathValue := in.FilePath
	if pathValue == "" {
		pathValue = in.Path
	}
	if pathValue == "" {
		return nil, nil
	}

	path := pathValue
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)

	before, exists := "", false
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		before, exists = string(b), true
	}

	switch toolName {
	case "Write":
		content := in.Content
		if content == nil {
			content = in.Contents
		}
		if content == nil {
			return nil, fmt.Errorf("Write input for %s has no content", path)
		}
		return []change{{path: path, before: before, after: *content, exists: exists}}, nil
	case "Edit":
		if !exists {
			return nil, nil
		}
		after, ok := applyEdit(before, in.OldString, in.NewString, in.ReplaceAll)
		if !ok {
			return nil, nil
		}
		return []change{{path: path, before: before, after: after, exists: exists}}, nil
	case "MultiEdit":
		if !exists {
			return nil, nil
		}
		after := before
		for _, e := range in.Edits {
			var ok bool
			after, ok = applyEdit(after, e.OldString, e.NewString, e.ReplaceAll)
			if !ok {
				return nil, nil
			}
		}
		return []change{{path: path, before: before, after: after, exists: exists}}, nil
	case "Delete":
		if !exists {
			return nil, nil
		}
		return []change{{path: path, before: before, exists: true, deleted: true}}, nil
	}
	return nil, nil
}

func proposedChange(toolName string, toolInput json.RawMessage, cwd string) *change {
	changes, err := proposedChanges(toolName, toolInput, cwd)
	if err != nil || len(changes) != 1 {
		return nil
	}
	return &changes[0]
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
