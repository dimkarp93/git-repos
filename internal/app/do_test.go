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
	if len(targets) != 2 || filepath.Base(targets[0]) != "a" || filepath.Base(targets[1]) != "b" {
		t.Fatalf("targets = %v", targets)
	}
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
	if len(targets) != 1 || filepath.Base(targets[0]) != "dirty" {
		t.Fatalf("targets = %v", targets)
	}
}

func TestExecAllContinuesAfterFailure(t *testing.T) {
	root := t.TempDir()
	first := newRepo(t, root, "o/a")
	second := newRepo(t, root, "o/b")
	var out strings.Builder
	s := doSession(t, root, &out)

	if code := execAll(s, []string{first, second}, "exit 3"); code != ExitFailure {
		t.Fatalf("code = %d", code)
	}
	text := out.String()
	if !strings.Contains(text, "[1/2] :: a") || !strings.Contains(text, "[2/2] :: b") {
		t.Fatalf("out = %q", text)
	}
	require.Contains(t, text, "done — 0, errors — 2", "out = %q", text)
}

func TestExecAllPassesEnvAndDir(t *testing.T) {
	root := t.TempDir()
	dir := newRepo(t, root, "o/a")
	marker := filepath.Join(dir, "marker")
	var out strings.Builder
	s := doSession(t, root, &out)

	if code := execAll(s, []string{dir}, "printf '%s' \"$GIT_REPOS_NAME\" > marker"); code != ExitOK {
		t.Fatalf("code = %d", code)
	}
	data, err := os.ReadFile(marker)
	require.NoError(t, err)
	require.Equal(t, "a", string(data), "marker = %q", data)
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
