package main

import (
	"path/filepath"
	"slices"
	"strings"
)

// normalize drops blank lines and trailing whitespace from each line, keeping
// leading indentation. Two sources that normalize equal differ only in blank
// lines / trailing whitespace — the churn the gate should skip.
func normalize(code string) string {
	var b strings.Builder
	first := true
	for ln := range strings.SplitSeq(code, "\n") {
		trimmed := strings.TrimRight(ln, " \t\r")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		if !first {
			b.WriteByte('\n')
		}
		b.WriteString(trimmed)
		first = false
	}
	return b.String()
}

// isWhitespaceOnlyChange reports whether before and after differ only in blank
// lines or trailing whitespace. Runs for EVERY file type. Leading indent
// survives, so a real re-indent (meaningful in Python/YAML) still gates.
func isWhitespaceOnlyChange(before, after string) bool {
	return normalize(before) == normalize(after)
}

// isCommentOnlyChange reports whether before and after have identical code once
// comments are stripped. Unknown file types return false: biased to gate rather
// than skip real code.
func isCommentOnlyChange(path, before, after string) bool {
	syn, ok := syntaxByExt[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return false
	}
	return normalize(stripComments(before, syn)) == normalize(stripComments(after, syn))
}

// stripComments blanks every comment/docstring, replacing each with its own
// newlines so line structure is preserved. Code and string literals survive.
func stripComments(src string, syn langSyntax) string {
	var b strings.Builder
	prev := 0
	for _, c := range scanComments(src, syn) {
		b.WriteString(src[prev:c.start])
		b.WriteString(strings.Repeat("\n", strings.Count(src[c.start:c.end], "\n")))
		prev = c.end
	}
	b.WriteString(src[prev:])
	return b.String()
}

// isTestFile matches test files by naming convention (Go _test.go, pytest
// test_*.py/conftest.py, *.test/*.spec.{js,ts}, *Test.java, a __tests__/ dir).
// Filename-only, so it never mis-skips a non-test file.
func isTestFile(path string) bool {
	name := filepath.Base(path)
	if name == "conftest.py" || (strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py")) {
		return true
	}
	for _, suf := range []string{"_test.go", "_test.py", "_test.rb", "_spec.rb", "Test.java", "Tests.java", "Test.kt"} {
		if strings.HasSuffix(name, suf) {
			return true
		}
	}
	for _, ext := range []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs"} {
		if strings.HasSuffix(name, ".test"+ext) || strings.HasSuffix(name, ".spec"+ext) {
			return true
		}
	}
	return slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "__tests__")
}

// skipPath reports whether to leave a target alone regardless of content:
// agent memory, session scratchpads, and plan files are Claude's own
// artifacts, not project code a human needs to review.
func skipPath(target string) bool {
	if strings.Contains(target, "/.claude/projects/") && strings.Contains(target, "/memory/") {
		return true
	}
	if strings.HasPrefix(target, "/tmp/claude-") || strings.HasPrefix(target, "/private/tmp/claude-") {
		return true
	}
	return strings.Contains(target, "/.claude/plans/")
}
