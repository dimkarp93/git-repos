package scan

import (
	"fmt"
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

func find(t *testing.T, opts Options) []string {
	t.Helper()

	repos, err := Find(opts)
	require.NoError(t, err)

	paths := make([]string, 0, len(repos))
	for _, repo := range repos {
		paths = append(paths, repo.Path)
	}
	return paths
}

func realPath(t *testing.T, path string) string {
	t.Helper()

	real, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)

	return real
}

func TestFindStopsAtRepoRoot(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "a"))
	mkRepo(t, filepath.Join(root, "a", "nested"))
	mkRepo(t, filepath.Join(root, "b", "deep", "c"))
	mkRepo(t, filepath.Join(root, "b", "node_modules", "pkg"))
	mkRepo(t, filepath.Join(root, ".hidden", "repo"))

	got := find(t, Options{Roots: []string{root}, Ignore: []string{"node_modules"}})

	real := realPath(t, root)
	assert.Equal(t, []string{filepath.Join(real, "a"), filepath.Join(real, "b", "deep", "c")}, got)
}

func TestFindRootItselfIsRepo(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, root)
	mkRepo(t, filepath.Join(root, "inner"))

	assert.Len(t, find(t, Options{Roots: []string{root}}), 1)
}

func TestFindRespectsMaxDepth(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "one", "two", "three"))

	cases := map[int]int{2: 0, 3: 1}
	for depth, want := range cases {
		t.Run(fmt.Sprint(depth), func(t *testing.T) {
			assert.Len(t, find(t, Options{Roots: []string{root}, MaxDepth: depth}), want)
		})
	}
}

func TestFindReportsDirsAndRepos(t *testing.T) {
	root := t.TempDir()
	mkRepo(t, filepath.Join(root, "a"))
	var dirs, repos int

	find(t, Options{Roots: []string{root}, OnDir: func(string) { dirs++ }, OnRepo: func(string) { repos++ }})

	assert.Equal(t, []int{2, 1}, []int{dirs, repos})
}

func TestFindMissingRootFails(t *testing.T) {
	_, err := Find(Options{Roots: []string{filepath.Join(t.TempDir(), "missing")}})

	assert.Error(t, err)
}
