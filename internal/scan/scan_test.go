package scan

import (
	"os"
	"path/filepath"
	"testing"
)

func mkRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestFindStopsAtRepoRoot(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "a"))
	mkRepo(t, filepath.Join(root, "a", "nested"))
	mkRepo(t, filepath.Join(root, "b", "deep", "c"))
	if err := os.MkdirAll(filepath.Join(root, "b", "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	mkRepo(t, filepath.Join(root, "b", "node_modules", "pkg"))
	mkRepo(t, filepath.Join(root, ".hidden", "repo"))

	repos, err := Find(Options{Roots: []string{root}, Ignore: []string{"node_modules"}})
	if err != nil {
		t.Fatal(err)
	}
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(real, "a"), filepath.Join(real, "b", "deep", "c")}
	if len(repos) != len(want) {
		t.Fatalf("repos = %v, want %v", repos, want)
	}
	for i, repo := range repos {
		if repo.Path != want[i] {
			t.Errorf("repos[%d] = %q, want %q", i, repo.Path, want[i])
		}
	}
}

func TestFindRootItselfIsRepo(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	mkRepo(t, filepath.Join(root, "inner"))
	repos, err := Find(Options{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("repos = %v, want only the root", repos)
	}
}

func TestFindRespectsMaxDepth(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "one", "two", "three"))
	repos, err := Find(Options{Roots: []string{root}, MaxDepth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 0 {
		t.Fatalf("repos = %v, want none", repos)
	}
	repos, err = Find(Options{Roots: []string{root}, MaxDepth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("repos = %v, want one", repos)
	}
}
