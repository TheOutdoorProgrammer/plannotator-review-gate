package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestProposedChange(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "foo.go")
	if err := os.WriteFile(existing, []byte("package main\nvar x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(dir, "new.go")

	t.Run("write to existing", func(t *testing.T) {
		ch := proposedChange("Write", mustJSON(t, map[string]any{
			"file_path": existing, "content": "package main\nvar x = 2\n",
		}), dir)
		if ch == nil || !ch.exists || ch.after != "package main\nvar x = 2\n" {
			t.Fatalf("unexpected change: %+v", ch)
		}
		if ch.before != "package main\nvar x = 1\n" {
			t.Errorf("before = %q", ch.before)
		}
	})

	t.Run("write to new file", func(t *testing.T) {
		ch := proposedChange("Write", mustJSON(t, map[string]any{
			"file_path": newFile, "content": "package main\n",
		}), dir)
		if ch == nil || ch.exists || ch.before != "" || ch.after != "package main\n" {
			t.Fatalf("unexpected change: %+v", ch)
		}
	})

	t.Run("relative file_path resolves against cwd", func(t *testing.T) {
		ch := proposedChange("Write", mustJSON(t, map[string]any{
			"file_path": "foo.go", "content": "package main\nvar x = 3\n",
		}), dir)
		if ch == nil || ch.path != existing {
			t.Fatalf("want path %q, got %+v", existing, ch)
		}
	})

	t.Run("edit with matching old_string", func(t *testing.T) {
		ch := proposedChange("Edit", mustJSON(t, map[string]any{
			"file_path": existing, "old_string": "var x = 1", "new_string": "var x = 99",
		}), dir)
		if ch == nil || ch.after != "package main\nvar x = 99\n" {
			t.Fatalf("unexpected change: %+v", ch)
		}
	})

	t.Run("edit with non-matching old_string defers", func(t *testing.T) {
		ch := proposedChange("Edit", mustJSON(t, map[string]any{
			"file_path": existing, "old_string": "not present", "new_string": "y",
		}), dir)
		if ch != nil {
			t.Fatalf("expected nil, got %+v", ch)
		}
	})

	t.Run("multiedit applies in sequence", func(t *testing.T) {
		ch := proposedChange("MultiEdit", mustJSON(t, map[string]any{
			"file_path": existing,
			"edits": []map[string]any{
				{"old_string": "package main", "new_string": "package foo"},
				{"old_string": "var x = 1", "new_string": "var x = 2"},
			},
		}), dir)
		if ch == nil || ch.after != "package foo\nvar x = 2\n" {
			t.Fatalf("unexpected change: %+v", ch)
		}
	})

	t.Run("edit to missing file defers", func(t *testing.T) {
		ch := proposedChange("Edit", mustJSON(t, map[string]any{
			"file_path": newFile, "old_string": "x", "new_string": "y",
		}), dir)
		if ch != nil {
			t.Fatalf("expected nil, got %+v", ch)
		}
	})

	t.Run("no file_path defers", func(t *testing.T) {
		if ch := proposedChange("Write", mustJSON(t, map[string]any{"content": "x"}), dir); ch != nil {
			t.Fatalf("expected nil, got %+v", ch)
		}
	})

	t.Run("unknown tool defers", func(t *testing.T) {
		ch := proposedChange("NotebookEdit", mustJSON(t, map[string]any{
			"file_path": existing, "content": "x",
		}), dir)
		if ch != nil {
			t.Fatalf("expected nil, got %+v", ch)
		}
	})
}

func TestApplyEdit(t *testing.T) {
	if _, ok := applyEdit("abc", "", "x", false); ok {
		t.Error("empty old_string should not apply")
	}
	got, ok := applyEdit("a a a", "a", "b", true)
	if !ok || got != "b b b" {
		t.Errorf("replaceAll: got %q ok=%v", got, ok)
	}
	got, ok = applyEdit("a a a", "a", "b", false)
	if !ok || got != "b a a" {
		t.Errorf("first-only: got %q ok=%v", got, ok)
	}
}
