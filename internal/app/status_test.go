package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/multi"
	"github.com/dimkarp93/git-repos/internal/scan"
)

func fakeMulti(reals map[string][]multi.Workspace) multi.Lister {
	return func(_ context.Context, dir string) ([]multi.Workspace, error) {
		main := dir
		for repo, list := range reals {
			for _, ws := range list {
				if ws.Path == dir {
					main = repo
				}
			}
		}
		out := []multi.Workspace{{Name: "main", Kind: multi.Main, Path: main}}
		return append(out, reals[main]...), nil
	}
}

func failingMulti(err error) multi.Lister {
	return func(context.Context, string) ([]multi.Workspace, error) { return nil, err }
}

func statusRepo(t *testing.T, root, name string) string {
	t.Helper()
	dir := newRepo(t, root, "o/"+name)
	commit(t, dir, "a.txt")
	return dir
}

func TestStatusAllExpandsRealWorkspaces(t *testing.T) {
	root := t.TempDir()
	app := statusRepo(t, root, "app")
	feat := filepath.Join(t.TempDir(), "feat", "app")
	list := fakeMulti(map[string][]multi.Workspace{app: {
		{Name: "feat", Kind: multi.Real, Path: feat, Branch: "feat", Dirty: true},
		{Name: "gone", Kind: multi.Real, Path: "/nowhere", Branch: "gone", Missing: true},
		{Name: "loose", Kind: multi.Real, Path: "/loose"},
	}})

	results := statusAll(t.Context(), list, []scan.Repo{{Path: app}}, options{jobs: 1}, &progress{})

	require.Len(t, results, 3)
	t.Run("main first", func(t *testing.T) {
		assert.Equal(t, statusResult{dir: app, repo: app, name: "app", path: shortPath(app), workspace: "main", branch: "main"}, results[0])
	})
	t.Run("real workspace", func(t *testing.T) {
		assert.Equal(t, statusResult{dir: feat, repo: app, name: "app", path: shortPath(feat), workspace: "feat", branch: "feat", feature: true, real: true}, results[1])
	})
	t.Run("detached real workspace", func(t *testing.T) {
		assert.Equal(t, "detached", results[2].branch)
	})
}

func TestStatusAllIgnoresDirtyRealWorkspace(t *testing.T) {
	root := t.TempDir()
	app := statusRepo(t, root, "app")
	list := fakeMulti(map[string][]multi.Workspace{app: {
		{Name: "feat", Kind: multi.Real, Path: "/wt/feat", Branch: "feat", Dirty: true},
	}})

	results := statusAll(t.Context(), list, []scan.Repo{{Path: app}}, options{jobs: 1}, &progress{})

	require.Len(t, results, 2)
	assert.Equal(t, "[feature] [wip]", markCell(results[1]).Text)
}

func TestStatusAllDeduplicatesLinkedWorktree(t *testing.T) {
	root := t.TempDir()
	app := statusRepo(t, root, "app")
	feat := statusRepo(t, root, "feat")
	list := fakeMulti(map[string][]multi.Workspace{app: {
		{Name: "feat", Kind: multi.Real, Path: feat, Branch: "feat"},
	}})

	results := statusAll(t.Context(), list, []scan.Repo{{Path: app}, {Path: feat}}, options{jobs: 1}, &progress{})

	assert.Equal(t, []string{"main", "feat"}, workspaceNames(results))
}

func TestStatusAllShowsRepoFoundThroughWorktree(t *testing.T) {
	root := t.TempDir()
	feat := statusRepo(t, root, "feat")
	app := statusRepo(t, t.TempDir(), "app")
	list := fakeMulti(map[string][]multi.Workspace{app: {
		{Name: "feat", Kind: multi.Real, Path: feat, Branch: "feat"},
	}})

	results := statusAll(t.Context(), list, []scan.Repo{{Path: feat}}, options{jobs: 1}, &progress{})

	require.Len(t, results, 2)
	assert.Equal(t, app, results[0].dir)
}

func TestStatusAllReportsMultiFailure(t *testing.T) {
	root := t.TempDir()
	app := statusRepo(t, root, "app")

	results := statusAll(t.Context(), failingMulti(errors.New("git-multi ls: no origin")), []scan.Repo{{Path: app}}, options{jobs: 1}, &progress{})

	require.Len(t, results, 1)
	assert.EqualError(t, results[0].err, "git-multi ls: no origin")
	assert.Equal(t, app, results[0].dir)
}

func TestStatusAllRequiresMainWorkspace(t *testing.T) {
	root := t.TempDir()
	app := statusRepo(t, root, "app")
	list := func(context.Context, string) ([]multi.Workspace, error) {
		return []multi.Workspace{{Name: "feat", Kind: multi.Real, Path: "/wt/feat"}}, nil
	}

	results := statusAll(t.Context(), list, []scan.Repo{{Path: app}}, options{jobs: 1}, &progress{})

	require.Len(t, results, 1)
	assert.ErrorContains(t, results[0].err, "no main workspace")
}

func TestStatusSummaryCountsWorkspaces(t *testing.T) {
	results := []statusResult{
		{repo: "/a", workspace: "main", feature: false, dirty: true},
		{repo: "/a", workspace: "feat", feature: true, real: true},
		{repo: "/b", err: errors.New("boom")},
	}

	_, sum := statusRows(results, filters{})

	assert.Equal(t, statusSummary{shown: 3, repos: 2, workspaces: 1, features: 1, dirty: 1, failed: 1}, sum)
}

func TestSelectReposRunsOnlyInMainWorkspace(t *testing.T) {
	root := t.TempDir()
	app := statusRepo(t, root, "app")
	lib := statusRepo(t, root, "lib")
	feat := filepath.Join(t.TempDir(), "feat", "app")
	var out strings.Builder
	s := doSession(t, root, &out)
	s.multi = fakeMulti(map[string][]multi.Workspace{app: {
		{Name: "feat", Kind: multi.Real, Path: feat, Branch: "feat"},
	}})

	t.Run("without filters", func(t *testing.T) {
		targets, err := selectRepos(s, options{jobs: 1})
		require.NoError(t, err)
		assert.Equal(t, []string{app, lib}, targets)
	})
	t.Run("real workspace does not select", func(t *testing.T) {
		targets, err := selectRepos(s, options{jobs: 1, filters: filters{feature: true}})
		require.NoError(t, err)
		assert.Empty(t, targets)
	})
	t.Run("main workspace selects", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(lib, "b.txt"), []byte("x"), 0o644))
		targets, err := selectRepos(s, options{jobs: 1, filters: filters{wip: true}})
		require.NoError(t, err)
		assert.Equal(t, []string{lib}, targets)
	})
}

func TestHelpRequested(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"-h"}, true},
		{[]string{"--wip", "--help"}, true},
		{[]string{"-help"}, true},
		{[]string{"--", "git", "-h"}, false},
		{[]string{"--wip"}, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, helpRequested(tc.args), tc.args)
	}
}

func workspaceNames(results []statusResult) []string {
	out := make([]string, 0, len(results))
	for _, res := range results {
		out = append(out, res.workspace)
	}
	return out
}
