package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func parseApplyPatch(patch, cwd string) ([]change, error) {
	lines := strings.Split(strings.ReplaceAll(patch, "\r\n", "\n"), "\n")
	if len(lines) < 2 || lines[0] != "*** Begin Patch" {
		return nil, fmt.Errorf("patch is missing the Begin Patch marker")
	}
	last := len(lines) - 1
	for last > 0 && lines[last] == "" {
		last--
	}
	if lines[last] != "*** End Patch" {
		return nil, fmt.Errorf("patch is missing the End Patch marker")
	}
	lines = lines[:last+1]

	var changes []change
	seen := map[string]bool{}
	add := func(ch change) error {
		if seen[ch.path] {
			return fmt.Errorf("patch changes %s more than once", ch.path)
		}
		seen[ch.path] = true
		changes = append(changes, ch)
		return nil
	}

	for i := 1; i < len(lines); {
		line := lines[i]
		switch {
		case line == "":
			i++
			continue
		case line == "*** End Patch":
			i = len(lines)
			continue
		case strings.HasPrefix(line, "*** Add File: "):
			path, err := safePatchPath(cwd, strings.TrimPrefix(line, "*** Add File: "))
			if err != nil {
				return nil, err
			}
			if _, err := os.Lstat(path); err == nil {
				return nil, fmt.Errorf("patch adds existing path %s", path)
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("inspect %s: %w", path, err)
			}
			i++
			var content []string
			for i < len(lines) && !strings.HasPrefix(lines[i], "*** ") {
				if !strings.HasPrefix(lines[i], "+") {
					return nil, fmt.Errorf("added file %s has a line without +", path)
				}
				content = append(content, strings.TrimPrefix(lines[i], "+"))
				i++
			}
			after := strings.Join(content, "\n")
			if len(content) > 0 {
				after += "\n"
			}
			if err := add(change{path: path, after: after}); err != nil {
				return nil, err
			}
		case strings.HasPrefix(line, "*** Delete File: "):
			path, err := safePatchPath(cwd, strings.TrimPrefix(line, "*** Delete File: "))
			if err != nil {
				return nil, err
			}
			before, err := readPatchFile(path)
			if err != nil {
				return nil, err
			}
			if err := add(change{path: path, before: before, exists: true, deleted: true}); err != nil {
				return nil, err
			}
			i++
		case strings.HasPrefix(line, "*** Update File: "):
			source, err := safePatchPath(cwd, strings.TrimPrefix(line, "*** Update File: "))
			if err != nil {
				return nil, err
			}
			before, err := readPatchFile(source)
			if err != nil {
				return nil, err
			}
			after := before
			moveTo := ""
			i++
			hunks := 0
			cursor := 0
			var anchors []string
			for i < len(lines) && lines[i] != "*** End Patch" &&
				!strings.HasPrefix(lines[i], "*** Add File: ") &&
				!strings.HasPrefix(lines[i], "*** Delete File: ") &&
				!strings.HasPrefix(lines[i], "*** Update File: ") {
				if strings.HasPrefix(lines[i], "*** Move to: ") {
					moveTo, err = safePatchPath(cwd, strings.TrimPrefix(lines[i], "*** Move to: "))
					if err != nil {
						return nil, err
					}
					i++
					continue
				}
				if lines[i] == "*** End of File" {
					i++
					continue
				}
				if !strings.HasPrefix(lines[i], "@@") {
					return nil, fmt.Errorf("update for %s has content outside a hunk", source)
				}
				if anchor := strings.TrimSpace(strings.TrimPrefix(lines[i], "@@")); anchor != "" {
					anchors = append(anchors, anchor)
				}
				i++
				var oldLines, newLines []string
				for i < len(lines) && !strings.HasPrefix(lines[i], "@@") &&
					!strings.HasPrefix(lines[i], "*** ") {
					if lines[i] == "" {
						return nil, fmt.Errorf("hunk for %s has an unprefixed blank line", source)
					}
					switch lines[i][0] {
					case ' ':
						oldLines = append(oldLines, lines[i][1:])
						newLines = append(newLines, lines[i][1:])
					case '-':
						oldLines = append(oldLines, lines[i][1:])
					case '+':
						newLines = append(newLines, lines[i][1:])
					default:
						return nil, fmt.Errorf("hunk for %s has invalid prefix %q", source, lines[i][0])
					}
					i++
				}
				if len(oldLines) == 0 && len(newLines) == 0 {
					continue
				}
				var ok bool
				atEOF := i < len(lines) && lines[i] == "*** End of File"
				after, cursor, ok = replacePatchHunk(after, oldLines, newLines, anchors, cursor, atEOF)
				if !ok {
					return nil, fmt.Errorf("hunk context did not match %s", source)
				}
				anchors = nil
				hunks++
			}
			if hunks == 0 {
				return nil, fmt.Errorf("update for %s has no hunks", source)
			}
			if moveTo == "" {
				if err := add(change{path: source, before: before, after: after, exists: true}); err != nil {
					return nil, err
				}
				continue
			}
			if _, err := os.Lstat(moveTo); err == nil {
				return nil, fmt.Errorf("patch moves to existing path %s", moveTo)
			} else if !os.IsNotExist(err) {
				return nil, fmt.Errorf("inspect %s: %w", moveTo, err)
			}
			if err := add(change{path: source, before: before, exists: true, deleted: true}); err != nil {
				return nil, err
			}
			if err := add(change{path: moveTo, after: after}); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported patch line %q", line)
		}
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("patch contains no file changes")
	}
	return changes, nil
}

func replacePatchHunk(
	src string,
	oldLines, newLines, anchors []string,
	cursor int,
	atEOF bool,
) (string, int, bool) {
	searchFrom := cursor
	for _, anchor := range anchors {
		var ok bool
		searchFrom, ok = findPatchAnchor(src, anchor, searchFrom)
		if !ok {
			return "", 0, false
		}
	}

	oldText := strings.Join(oldLines, "\n")
	newText := strings.Join(newLines, "\n")
	if oldText == "" {
		if atEOF {
			searchFrom = len(src)
		}
		if searchFrom < 0 || searchFrom > len(src) {
			return "", 0, false
		}
		if len(newLines) > 0 {
			newText += "\n"
		}
		out := src[:searchFrom] + newText + src[searchFrom:]
		return out, searchFrom + len(newText), true
	}
	for _, suffix := range []string{"\n", ""} {
		oldCandidate := oldText + suffix
		offset := strings.Index(src[searchFrom:], oldCandidate)
		if offset < 0 {
			continue
		}
		offset += searchFrom
		newCandidate := newText
		if suffix != "" && len(newLines) > 0 {
			newCandidate += suffix
		}
		out := src[:offset] + newCandidate + src[offset+len(oldCandidate):]
		return out, offset + len(newCandidate), true
	}
	return "", 0, false
}

func findPatchAnchor(src, anchor string, start int) (int, bool) {
	offset := start
	for offset <= len(src) {
		next := strings.IndexByte(src[offset:], '\n')
		end := len(src)
		if next >= 0 {
			end = offset + next
		}
		if strings.TrimSpace(src[offset:end]) == anchor {
			if end < len(src) {
				return end + 1, true
			}
			return end, true
		}
		if next < 0 {
			break
		}
		offset = end + 1
	}
	return 0, false
}

func readPatchFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("patch path is not a regular file: %s", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(body), nil
}

func safePatchPath(cwd, value string) (string, error) {
	if cwd == "" {
		return "", fmt.Errorf("patch event has no working directory")
	}
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) {
		return "", fmt.Errorf("patch path must be relative to the workspace: %q", value)
	}
	clean := filepath.Clean(value)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("patch path escapes the workspace: %q", value)
	}
	root, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve workspace: %w", err)
	}
	target := filepath.Join(root, clean)
	for current := target; current != root; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("inspect %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("patch path crosses a symlink: %s", current)
		}
	}
	return target, nil
}
