package gitcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	runGit(t, repo, "init", "--quiet", "--initial-branch=main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "test")
	runGit(t, repo, "commit", "--quiet", "--allow-empty", "-m", "init")
	return repo
}

func assertSamePath(t *testing.T, got, want string) {
	t.Helper()
	gotReal, err := filepath.EvalSymlinks(got)
	require.NoError(t, err)
	wantReal, err := filepath.EvalSymlinks(want)
	require.NoError(t, err)
	assert.Equal(t, wantReal, gotReal, "path = %q, want %q", gotReal, wantReal)
}

func mustWorktrees(t *testing.T, dir string) []Worktree {
	t.Helper()
	list, err := Worktrees(context.Background(), dir)
	require.NoError(t, err)
	return list
}

func TestParseVersion(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		want    Version
		wantErr bool
	}{
		{name: "plain", line: "git version 2.39.5", want: Version{Major: 2, Minor: 39}},
		{name: "windows", line: "git version 2.43.0.windows.1", want: Version{Major: 2, Minor: 43}},
		{name: "apple", line: "git version 2.39.3 (Apple Git-145)", want: Version{Major: 2, Minor: 39}},
		{name: "garbage", line: "not a version", wantErr: true},
		{name: "empty", line: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseVersion(tc.line)
			if tc.wantErr {
				require.Error(t, err, "parseVersion(%q) = %+v, want error", tc.line, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, "parseVersion(%q) = %+v, want %+v", tc.line, got, tc.want)
		})
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		name         string
		v            Version
		major, minor int
		want         bool
	}{
		{name: "newer minor", v: Version{Major: 2, Minor: 39}, major: 2, minor: 31, want: true},
		{name: "older minor", v: Version{Major: 2, Minor: 30}, major: 2, minor: 31, want: false},
		{name: "newer major", v: Version{Major: 3, Minor: 0}, major: 2, minor: 31, want: true},
		{name: "equal", v: Version{Major: 2, Minor: 31}, major: 2, minor: 31, want: true},
		{name: "minor 99 of old major", v: Version{Major: 1, Minor: 99}, major: 2, minor: 0, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.v.AtLeast(tc.major, tc.minor))
		})
	}
}

func TestGitVersion(t *testing.T) {
	v, err := GitVersion(context.Background())

	require.NoError(t, err)
	assert.GreaterOrEqual(t, v.Major, 2)
}

func linkedWorktree(t *testing.T, wtName string, args ...string) (string, string, []Worktree) {
	t.Helper()

	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), wtName)
	runGit(t, repo, append(append([]string{"worktree", "add"}, args...), wt)...)

	list := mustWorktrees(t, repo)
	require.Len(t, list, 2)
	return repo, wt, list
}

type worktreeView struct {
	Branch                           string
	Detached, Prunable, Locked, Bare bool
}

func viewOf(w Worktree) worktreeView {
	return worktreeView{Branch: w.Branch, Detached: w.Detached, Prunable: w.Prunable, Locked: w.Locked, Bare: w.Bare}
}

func TestWorktreesMainAndLinkedBranch(t *testing.T) {
	repo, wt, list := linkedWorktree(t, "wt", "-b", "feature")

	t.Run("main path", func(t *testing.T) {
		assertSamePath(t, list[0].Path, repo)
	})
	t.Run("main state", func(t *testing.T) {
		assert.Equal(t, worktreeView{Branch: "main"}, viewOf(list[0]))
	})
	t.Run("main head", func(t *testing.T) {
		assert.Equal(t, runGit(t, repo, "rev-parse", "HEAD"), list[0].Head)
	})
	t.Run("linked path", func(t *testing.T) {
		assertSamePath(t, list[1].Path, wt)
	})
	t.Run("linked state", func(t *testing.T) {
		assert.Equal(t, worktreeView{Branch: "feature"}, viewOf(list[1]))
	})
}

func TestWorktreesDetached(t *testing.T) {
	repo, wt, list := linkedWorktree(t, "wt", "--detach")

	t.Run("path", func(t *testing.T) {
		assertSamePath(t, list[1].Path, wt)
	})
	t.Run("state", func(t *testing.T) {
		assert.Equal(t, worktreeView{Detached: true}, viewOf(list[1]))
	})
	t.Run("head", func(t *testing.T) {
		assert.Equal(t, runGit(t, repo, "rev-parse", "HEAD"), list[1].Head)
	})
}

func requireGit(t *testing.T, major, minor int) {
	t.Helper()

	v, err := GitVersion(context.Background())
	require.NoError(t, err)
	if !v.AtLeast(major, minor) {
		t.Skipf("git %+v is older than %d.%d", v, major, minor)
	}
}

func TestWorktreesPrunable(t *testing.T) {
	requireGit(t, 2, 31)
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "gone", wt)
	require.NoError(t, os.RemoveAll(wt))

	list := mustWorktrees(t, repo)

	require.Len(t, list, 2)
	assert.Equal(t, worktreeView{Branch: "gone", Prunable: true}, viewOf(list[1]))
}

func TestWorktreesLocked(t *testing.T) {
	repo, wt, _ := linkedWorktree(t, "wt", "-b", "locked-branch")
	runGit(t, repo, "worktree", "lock", wt)

	list := mustWorktrees(t, repo)

	require.Len(t, list, 2)
	assert.Equal(t, worktreeView{Branch: "locked-branch", Locked: true}, viewOf(list[1]))
}

func TestWorktreesPathWithSpace(t *testing.T) {
	_, wt, list := linkedWorktree(t, "my worktree dir", "-b", "spaced")

	t.Run("path", func(t *testing.T) {
		assertSamePath(t, list[1].Path, wt)
	})
	t.Run("branch", func(t *testing.T) {
		assert.Equal(t, "spaced", list[1].Branch)
	})
}

func TestParseWorktrees(t *testing.T) {
	out := "HEAD ignored\nworktree /r\nHEAD aaa\nbranch refs/heads/main\r\n\nworktree /b\nbare\nlocked\nprunable gone\n"

	want := []Worktree{
		{Path: "/r", Head: "aaa", Branch: "main"},
		{Path: "/b", Bare: true, Locked: true, Prunable: true},
	}
	assert.Equal(t, want, parseWorktrees(out))
}
