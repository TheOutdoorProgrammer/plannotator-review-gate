package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseApplyPatchMultipleFiles(t *testing.T) {
	root := t.TempDir()
	update := filepath.Join(root, "update.txt")
	remove := filepath.Join(root, "remove.txt")
	if err := os.WriteFile(update, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remove, []byte("gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch := `*** Begin Patch
*** Update File: update.txt
@@
 one
-two
+TWO
 three
*** Add File: added.txt
+new
+file
*** Delete File: remove.txt
*** End Patch
`
	changes, err := parseApplyPatch(patch, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 {
		t.Fatalf("got %d changes, want 3: %+v", len(changes), changes)
	}
	if changes[0].after != "one\nTWO\nthree\n" {
		t.Errorf("updated content = %q", changes[0].after)
	}
	if changes[1].after != "new\nfile\n" || changes[1].exists {
		t.Errorf("added change = %+v", changes[1])
	}
	if !changes[2].deleted || changes[2].before != "gone\n" {
		t.Errorf("deleted change = %+v", changes[2])
	}
}

func TestParseApplyPatchMove(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "old.txt")
	if err := os.WriteFile(source, []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	patch := `*** Begin Patch
*** Update File: old.txt
*** Move to: nested/new.txt
@@
-old
+new
*** End Patch
`
	changes, err := parseApplyPatch(patch, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || !changes[0].deleted || changes[1].after != "new\n" {
		t.Fatalf("unexpected move: %+v", changes)
	}
	if changes[1].path != filepath.Join(root, "nested", "new.txt") {
		t.Errorf("move destination = %q", changes[1].path)
	}
}

func TestParseApplyPatchAnchorsAndEndOfFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "sample.go")
	if err := os.WriteFile(path, []byte("package sample\n\nfunc one() {\n\tx := 1\n}\n\nfunc two() {\n\tx := 1\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	patch := `*** Begin Patch
*** Update File: sample.go
@@ func two() {
 	x := 1
+	y := 2
 }
@@
+var end = true
*** End of File
*** End Patch
`
	changes, err := parseApplyPatch(patch, root)
	if err != nil {
		t.Fatal(err)
	}
	after := changes[0].after
	if strings.Count(after, "y := 2") != 1 ||
		!strings.Contains(after, "func two() {\n\tx := 1\n\ty := 2\n}") {
		t.Fatalf("anchor applied to the wrong location:\n%s", after)
	}
	if !strings.HasSuffix(after, "var end = true\n") {
		t.Fatalf("end-of-file insertion was misplaced:\n%s", after)
	}
}

func TestParseApplyPatchRejectsEscapesAndMismatches(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("actual\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"*** Begin Patch\n*** Add File: ../escape.txt\n+x\n*** End Patch\n",
		"*** Begin Patch\n*** Update File: file.txt\n@@\n-not-there\n+new\n*** End Patch\n",
		"*** Begin Patch\n*** Add File: unfinished.txt\n+x\n",
	}
	for _, patch := range cases {
		if _, err := parseApplyPatch(patch, root); err == nil {
			t.Errorf("expected rejection for:\n%s", patch)
		}
	}

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	patch := "*** Begin Patch\n*** Add File: link/escape.txt\n+x\n*** End Patch\n"
	_, err := parseApplyPatch(patch, root)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}
