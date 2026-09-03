package project

import (
	"os"
	"path/filepath"
	"testing"
)

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
		if got != nested {
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
		if got != root {
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
		if got != root {
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
		want := filepath.Clean(alias)
		if got != want {
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
		if got != cwd {
			t.Fatalf("root = %q, want %q", got, cwd)
		}
		alias, err := ProjectRoot(cwd)
		if err != nil {
			t.Fatal(err)
		}
		if alias != got {
			t.Fatalf("ProjectRoot = %q, DiscoverProjectRoot = %q", alias, got)
		}
	})
}
