package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A hostile global config stands in for a real developer's: signing on with a
// gpg that cannot run, plus a pre-commit hook that always fails.
func hostileGlobalConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	hooks := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(hooks, "pre-commit")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "gitconfig")
	body := "[commit]\n\tgpgsign = true\n[tag]\n\tgpgsign = true\n[gpg]\n\tprogram = " +
		filepath.Join(dir, "nonexistent-gpg") + "\n[core]\n\thooksPath = " + hooks + "\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

func TestStageRepoIgnoresGlobalSigningAndHooks(t *testing.T) {
	hostileGlobalConfig(t)

	cwd := t.TempDir()
	existing := filepath.Join(cwd, "pkg", "foo.go")
	if err := os.MkdirAll(filepath.Dir(existing), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		ch   *change
	}{
		{"existing file", &change{path: existing, before: "package main\n", after: "package main\nvar x = 1\n", exists: true}},
		{"new file", &change{path: filepath.Join(cwd, "new.go"), after: "package main\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := t.TempDir()
			if err := stageRepo(repo, tc.ch, cwd); err != nil {
				t.Fatalf("stageRepo: %v", err)
			}
			out, err := exec.Command("git", "-C", repo, "log", "--format=%G?", "-1").Output()
			if err != nil {
				t.Fatalf("git log: %v", err)
			}
			// "N" = no signature; anything else means the commit got signed.
			if got := strings.TrimSpace(string(out)); got != "N" {
				t.Errorf("commit signature status = %q, want %q", got, "N")
			}
		})
	}
}

// The staged repo's diff must be exactly the proposed edit, at the file's real
// path relative to the project — that path is what the reviewer sees.
func TestStageRepoDiff(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	hostileGlobalConfig(t)

	cwd := t.TempDir()
	cases := []struct {
		name     string
		ch       *change
		wantPath string
		wantAdd  string
	}{
		{
			name:     "existing nested file",
			ch:       &change{path: filepath.Join(cwd, "pkg", "foo.go"), before: "package main\n", after: "package main\nvar x = 1\n", exists: true},
			wantPath: "pkg/foo.go",
			wantAdd:  "+var x = 1",
		},
		{
			name:     "new file",
			ch:       &change{path: filepath.Join(cwd, "new.go"), after: "package main\n"},
			wantPath: "new.go",
			wantAdd:  "+package main",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(tc.ch.path), 0o755); err != nil {
				t.Fatal(err)
			}
			repo := t.TempDir()
			if err := stageRepo(repo, tc.ch, cwd); err != nil {
				t.Fatalf("stageRepo: %v", err)
			}
			out, err := exec.Command("git", "-C", repo, "diff", "HEAD").Output()
			if err != nil {
				t.Fatalf("git diff: %v", err)
			}
			diff := string(out)
			if !strings.Contains(diff, tc.wantPath) {
				t.Errorf("diff should name %q, got:\n%s", tc.wantPath, diff)
			}
			if !strings.Contains(diff, tc.wantAdd) {
				t.Errorf("diff should add %q, got:\n%s", tc.wantAdd, diff)
			}
		})
	}
}

// A file outside the project root falls back to its basename rather than
// leaking a ../.. path into the staged repo.
func TestStageRepoOutsideCWD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
	hostileGlobalConfig(t)

	outside := filepath.Join(t.TempDir(), "elsewhere.go")
	repo := t.TempDir()
	ch := &change{path: outside, after: "package main\n"}
	if err := stageRepo(repo, ch, t.TempDir()); err != nil {
		t.Fatalf("stageRepo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, "elsewhere.go")); err != nil {
		t.Errorf("expected the file staged at its basename: %v", err)
	}
}
