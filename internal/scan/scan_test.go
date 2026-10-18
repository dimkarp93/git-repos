package scan

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkRepo(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(path, ".git"), 0o755))
}

func TestFindStopsAtRepoRoot(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "a"))
	mkRepo(t, filepath.Join(root, "a", "nested"))
	mkRepo(t, filepath.Join(root, "b", "deep", "c"))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "b", "node_modules", "pkg"), 0o755))
	mkRepo(t, filepath.Join(root, "b", "node_modules", "pkg"))
	mkRepo(t, filepath.Join(root, ".hidden", "repo"))

	repos, err := Find(Options{Roots: []string{root}, Ignore: []string{"node_modules"}})
	require.NoError(t, err)
	real, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)
	want := []string{filepath.Join(real, "a"), filepath.Join(real, "b", "deep", "c")}
	require.Len(t, repos, len(want), "repos = %v, want %v", repos, want)
	for i, repo := range repos {
		assert.Equal(t, want[i], repo.Path, "repos[%d] = %q, want %q", i, repo.Path, want[i])
	}
}

func TestFindRootItselfIsRepo(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	mkRepo(t, filepath.Join(root, "inner"))
	repos, err := Find(Options{Roots: []string{root}})
	require.NoError(t, err)
	require.Len(t, repos, 1, "repos = %v, want only the root", repos)
}

func TestFindRespectsMaxDepth(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "one", "two", "three"))
	repos, err := Find(Options{Roots: []string{root}, MaxDepth: 2})
	require.NoError(t, err)
	require.Len(t, repos, 0, "repos = %v, want none", repos)
	repos, err = Find(Options{Roots: []string{root}, MaxDepth: 3})
	require.NoError(t, err)
	require.Len(t, repos, 1, "repos = %v, want one", repos)
}
