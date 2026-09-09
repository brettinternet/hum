package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCanonicalProjectIdentity(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "nested", "leaf")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{nested, filepath.Join(alias, "nested", "leaf")} {
		got, err := DiscoverProjectRoot(cwd)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Clean(want) {
			t.Fatalf("root(%q) = %q, want %q", cwd, got, want)
		}
	}
	scope, err := Resolve(filepath.Join(alias, "nested", "leaf"))
	if err != nil {
		t.Fatal(err)
	}
	if scope.Cwd != filepath.Join(alias, "nested", "leaf") {
		t.Fatalf("lexical cwd = %q", scope.Cwd)
	}
	fallback := filepath.Join(t.TempDir(), "fallback")
	if err := os.Mkdir(fallback, 0o700); err != nil {
		t.Fatal(err)
	}
	fallbackAlias := filepath.Join(t.TempDir(), "fallback-alias")
	if err := os.Symlink(fallback, fallbackAlias); err != nil {
		t.Fatal(err)
	}
	wantFallback, err := filepath.EvalSymlinks(fallback)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverProjectRoot(filepath.Join(fallbackAlias, "."))
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(wantFallback) {
		t.Fatalf("fallback = %q, want %q", got, wantFallback)
	}

	repository := filepath.Join(t.TempDir(), "repository")
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Mkdir(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", repository},
		{"-C", repository, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial"},
		{"-C", repository, "worktree", "add", "-b", "linked", linked},
	} {
		if output, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	mainRoot, err := DiscoverProjectRoot(repository)
	if err != nil {
		t.Fatal(err)
	}
	linkedRoot, err := DiscoverProjectRoot(linked)
	if err != nil {
		t.Fatal(err)
	}
	if mainRoot == linkedRoot {
		t.Fatalf("linked worktrees collapsed to %q", mainRoot)
	}
}

func TestDiscoverProjectRoot(t *testing.T) {
	t.Run("nearest git directory", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		nested := filepath.Join(root, "nested")
		if err := os.MkdirAll(filepath.Join(nested, "leaf"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(nested, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverProjectRoot(filepath.Join(nested, "leaf"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(nested)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("root = %q, want %q", got, nested)
		}
	})

	t.Run("git worktree file", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /tmp/worktree"), 0o600); err != nil {
			t.Fatal(err)
		}
		cwd := filepath.Join(root, "child", "leaf")
		if err := os.MkdirAll(cwd, 0o700); err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverProjectRoot(cwd)
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("root = %q, want %q", got, root)
		}
	})

	t.Run("file cwd probes its parent", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(root, "child", "manifest.txt")
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverProjectRoot(file)
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("root = %q, want %q", got, root)
		}
	})

	t.Run("symlink keeps lexical root", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, ".git"), 0o700); err != nil {
			t.Fatal(err)
		}
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(root, alias); err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverProjectRoot(filepath.Join(alias, "child"))
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(root)
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Clean(want) {
			t.Fatalf("root = %q, want lexical alias %q", got, want)
		}
	})

	t.Run("fallback is absolute and clean", func(t *testing.T) {
		base := t.TempDir()
		cwd := filepath.Join(base, "nested")
		if err := os.Mkdir(cwd, 0o700); err != nil {
			t.Fatal(err)
		}
		got, err := DiscoverProjectRoot(filepath.Join(cwd, "."))
		if err != nil {
			t.Fatal(err)
		}
		want, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("root = %q, want %q", got, cwd)
		}
	})
}
