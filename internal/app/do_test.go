package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/utils/pkg/collections"
)

func doSession(t *testing.T, root string, out *strings.Builder) *session {
	t.Helper()
	store, err := cache.Open(t.TempDir())
	require.NoError(t, err)
	return &session{
		ctx:     t.Context(),
		cfg:     config.Default(),
		roots:   []string{root},
		store:   store,
		printer: render.NewPrinter(out, false),
		pr:      &progress{},
		spinner: render.NewSpinner(out, false),
	}
}

func TestSelectReposWithoutFilters(t *testing.T) {
	root := t.TempDir()
	newRepo(t, root, "o/a")
	newRepo(t, root, "o/b")
	var out strings.Builder
	s := doSession(t, root, &out)

	targets, err := selectRepos(s, options{})

	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, baseNames(targets))
}

func baseNames(paths []string) []string {
	return collections.Map(paths, filepath.Base)
}

func TestSelectReposByInDevelop(t *testing.T) {
	root := t.TempDir()
	clean := newRepo(t, root, "o/clean")
	commit(t, clean, "a.txt")
	dirty := newRepo(t, root, "o/dirty")
	commit(t, dirty, "a.txt")
	require.NoError(t, os.WriteFile(filepath.Join(dirty, "b.txt"), []byte("x"), 0o644))
	var out strings.Builder
	s := doSession(t, root, &out)

	targets, err := selectRepos(s, options{jobs: 1, filters: filters{inDevelop: true}})

	require.NoError(t, err)
	assert.Equal(t, []string{"dirty"}, baseNames(targets))
}

func TestExecAllContinuesAfterFailure(t *testing.T) {
	root := t.TempDir()
	first := newRepo(t, root, "o/a")
	second := newRepo(t, root, "o/b")
	var out strings.Builder
	s := doSession(t, root, &out)

	code := execAll(s, []string{first, second}, "exit 3")

	t.Run("exit code", func(t *testing.T) {
		assert.Equal(t, ExitFailure, code)
	})
	for _, want := range []string{"[1/2] :: a", "[2/2] :: b", "done — 0, errors — 2"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out.String(), want)
		})
	}
}

func TestExecAllPassesEnvAndDir(t *testing.T) {
	root := t.TempDir()
	dir := newRepo(t, root, "o/a")
	marker := filepath.Join(dir, "marker")
	var out strings.Builder
	s := doSession(t, root, &out)

	code := execAll(s, []string{dir}, "printf '%s' \"$GIT_REPOS_NAME\" > marker")

	require.Equal(t, ExitOK, code)
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	assert.Equal(t, "a", string(data))
}

func TestShellScriptQuoting(t *testing.T) {
	cases := []struct {
		action []string
		want   string
	}{
		{[]string{"git status -s | wc -l"}, "git status -s | wc -l"},
		{[]string{"git", "status", "-s"}, "git status -s"},
		{[]string{"git", "commit", "-m", "two words"}, "git commit -m 'two words'"},
		{[]string{"echo", "it's"}, `echo 'it'\''s'`},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, shellScript(tc.action))
	}
}
