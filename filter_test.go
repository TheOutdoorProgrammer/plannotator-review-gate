package main

import (
	"strings"
	"testing"
)

func TestIsWhitespaceOnlyChange(t *testing.T) {
	cases := []struct {
		name          string
		before, after string
		want          bool
	}{
		{"blank line added to markdown", "# Title\ntext\n", "# Title\n\ntext\n", true},
		{"blank line added to json", "{\n  \"a\": 1\n}\n", "{\n\n  \"a\": 1\n}\n", true},
		{"trailing whitespace fix", "line one  \nline two\t\n", "line one\nline two\n", true},
		{"multiple blank lines collapse", "a\n\n\n\nb\n", "a\nb\n", true},
		{"identical", "same\n", "same\n", true},
		{"real code added (go)", "func f() {\n}\n", "func f() {\n\tx := 1\n}\n", false},
		{"real text added (md)", "# Title\n", "# Title\nnew paragraph\n", false},
		{"leading re-indent still gates", "def f():\n  x = 1\n", "def f():\n    x = 1\n", false},
	}
	for _, c := range cases {
		if got := isWhitespaceOnlyChange(c.before, c.after); got != c.want {
			t.Errorf("%s: isWhitespaceOnlyChange = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsCommentOnlyChange(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		before, after string
		want          bool
	}{
		{
			name:   "go line comment added",
			path:   "/x/foo.go",
			before: "package main\nfunc f() {}\n",
			after:  "package main\n// does nothing\nfunc f() {}\n",
			want:   true,
		},
		{
			name:   "go comment reworded",
			path:   "/x/foo.go",
			before: "// old note\nvar x = 1\n",
			after:  "// new note\nvar x = 1\n",
			want:   true,
		},
		{
			name:   "go block comment reworded",
			path:   "/x/foo.go",
			before: "/* old\n   note */\nvar x = 1\n",
			after:  "/* new\n   note */\nvar x = 1\n",
			want:   true,
		},
		{
			name:   "trailing comment added",
			path:   "/x/foo.go",
			before: "var x = 1\n",
			after:  "var x = 1 // why\n",
			want:   true,
		},
		{
			name:   "python docstring changed",
			path:   "/x/foo.py",
			before: "def f():\n    \"\"\"old\"\"\"\n    return 1\n",
			after:  "def f():\n    \"\"\"new doc\"\"\"\n    return 1\n",
			want:   true,
		},
		{
			name:   "comment token inside a string is code, not a comment",
			path:   "/x/foo.go",
			before: "var u = \"http://x\"\n",
			after:  "var u = \"http://y\"\n",
			want:   false,
		},
		{
			name:   "comment marker in a go raw string is not a comment",
			path:   "/x/foo.go",
			before: "var s = `a // b`\n",
			after:  "var s = `a // c`\n",
			want:   false,
		},
		{
			name:   "escaped quote does not end the string early",
			path:   "/x/foo.go",
			before: "var s = \"a\\\" // x\"\nvar y = 1\n",
			after:  "var s = \"a\\\" // x\"\nvar y = 2\n",
			want:   false,
		},
		{
			name:   "real code change alongside comment",
			path:   "/x/foo.go",
			before: "// note\nvar x = 1\n",
			after:  "// note\nvar x = 2\n",
			want:   false,
		},
		{
			name:   "comment removed and code changed together",
			path:   "/x/foo.go",
			before: "// note\nvar x = 1\n",
			after:  "var x = 2\n",
			want:   false,
		},
		{
			name:   "unknown file type never counts as comment-only",
			path:   "/x/foo.txt",
			before: "# a\nb\n",
			after:  "# c\nb\n",
			want:   false,
		},
		{
			name:   "extension matching is case-insensitive",
			path:   "/x/FOO.GO",
			before: "// old\nvar x = 1\n",
			after:  "// new\nvar x = 1\n",
			want:   true,
		},
	}
	for _, c := range cases {
		if got := isCommentOnlyChange(c.path, c.before, c.after); got != c.want {
			t.Errorf("%s: isCommentOnlyChange = %v, want %v", c.name, got, c.want)
		}
	}
}

// stripComments must preserve line structure, so a comment-only edit that also
// shifts line numbers is still recognized as comment-only.
func TestStripCommentsPreservesLineCount(t *testing.T) {
	src := "package main\n/* a\nb\nc */\nvar x = 1\n"
	stripped := stripComments(src, syntaxByExt[".go"])
	if want, got := strings.Count(src, "\n"), strings.Count(stripped, "\n"); want != got {
		t.Errorf("newline count changed: src %d, stripped %d (%q)", want, got, stripped)
	}
	if !strings.Contains(stripped, "var x = 1") {
		t.Errorf("stripped output lost the code: %q", stripped)
	}
}

func TestIsTestFile(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/x/foo_test.go", true},
		{"/x/test_foo.py", true},
		{"/x/conftest.py", true},
		{"/x/foo.test.ts", true},
		{"/x/foo.spec.jsx", true},
		{"/x/FooTest.java", true},
		{"/x/__tests__/foo.js", true},
		{"/x/foo.go", false},
		{"/x/foo.py", false},
		{"/x/testdata/foo.go", false},
	}
	for _, c := range cases {
		if got := isTestFile(c.path); got != c.want {
			t.Errorf("isTestFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestSkipPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/Users/x/.claude/projects/p/memory/note.md", true},
		{"/tmp/claude-501/scratch/x.go", true},
		{"/private/tmp/claude-501/scratch/x.go", true},
		{"/Users/x/.claude/plans/plan.md", true},
		{"/Users/x/projects/repo/main.go", false},
		{"/Users/x/.claude/projects/p/session.jsonl", false},
	}
	for _, c := range cases {
		if got := skipPath(c.path); got != c.want {
			t.Errorf("skipPath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
