package gitcmd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "--quiet", "--initial-branch=main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "test")
	runGit(t, repo, "commit", "--quiet", "--allow-empty", "-m", "init")
	return repo
}

func assertSamePath(t *testing.T, got, want string) {
	t.Helper()
	gotReal, err := filepath.EvalSymlinks(got)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", got, err)
	}
	wantReal, err := filepath.EvalSymlinks(want)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", want, err)
	}
	if gotReal != wantReal {
		t.Errorf("path = %q, want %q", gotReal, wantReal)
	}
}

func mustWorktrees(t *testing.T, dir string) []Worktree {
	t.Helper()
	list, err := Worktrees(context.Background(), dir)
	if err != nil {
		t.Fatalf("Worktrees: %v", err)
	}
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
				if err == nil {
					t.Fatalf("parseVersion(%q) = %+v, want error", tc.line, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVersion(%q): %v", tc.line, err)
			}
			if got != tc.want {
				t.Errorf("parseVersion(%q) = %+v, want %+v", tc.line, got, tc.want)
			}
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
			if got := tc.v.AtLeast(tc.major, tc.minor); got != tc.want {
				t.Errorf("%+v.AtLeast(%d, %d) = %v, want %v", tc.v, tc.major, tc.minor, got, tc.want)
			}
		})
	}
}

func TestGitVersion(t *testing.T) {
	v, err := GitVersion(context.Background())
	if err != nil {
		t.Fatalf("GitVersion: %v", err)
	}
	if v.Major < 2 {
		t.Errorf("GitVersion major = %d, want >= 2 (%+v)", v.Major, v)
	}
}

func TestWorktreesMainAndLinkedBranch(t *testing.T) {
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "feature", wt)

	list := mustWorktrees(t, repo)
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(list), list)
	}
	assertSamePath(t, list[0].Path, repo)
	if list[0].Branch != "main" || list[0].Detached {
		t.Errorf("main = %+v, want branch main, not detached", list[0])
	}
	if want := runGit(t, repo, "rev-parse", "HEAD"); list[0].Head != want {
		t.Errorf("main Head = %q, want %q", list[0].Head, want)
	}
	assertSamePath(t, list[1].Path, wt)
	if list[1].Branch != "feature" || list[1].Detached {
		t.Errorf("linked = %+v, want branch feature, not detached", list[1])
	}
	if list[1].Prunable || list[1].Locked || list[1].Bare {
		t.Errorf("linked flags = %+v, want none set", list[1])
	}
}

func TestWorktreesDetached(t *testing.T) {
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "--detach", wt)

	list := mustWorktrees(t, repo)
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(list), list)
	}
	assertSamePath(t, list[1].Path, wt)
	if !list[1].Detached {
		t.Errorf("Detached = false, want true: %+v", list[1])
	}
	if list[1].Branch != "" {
		t.Errorf("Branch = %q, want empty", list[1].Branch)
	}
	if want := runGit(t, repo, "rev-parse", "HEAD"); list[1].Head != want {
		t.Errorf("Head = %q, want %q", list[1].Head, want)
	}
}

func TestWorktreesPrunable(t *testing.T) {
	v, err := GitVersion(context.Background())
	if err != nil {
		t.Fatalf("GitVersion: %v", err)
	}
	if !v.AtLeast(2, 31) {
		t.Skipf("git %+v is older than 2.31", v)
	}
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "gone", wt)
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	list := mustWorktrees(t, repo)
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(list), list)
	}
	if !list[1].Prunable {
		t.Errorf("Prunable = false, want true: %+v", list[1])
	}
	if list[1].Branch != "gone" {
		t.Errorf("Branch = %q, want gone", list[1].Branch)
	}
}

func TestWorktreesLocked(t *testing.T) {
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	runGit(t, repo, "worktree", "add", "-b", "locked-branch", wt)
	runGit(t, repo, "worktree", "lock", wt)

	list := mustWorktrees(t, repo)
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(list), list)
	}
	if !list[1].Locked {
		t.Errorf("Locked = false, want true: %+v", list[1])
	}
	if list[1].Prunable {
		t.Errorf("Prunable = true, want false: %+v", list[1])
	}
}

func TestWorktreesPathWithSpace(t *testing.T) {
	repo := newRepo(t)
	wt := filepath.Join(t.TempDir(), "my worktree dir")
	runGit(t, repo, "worktree", "add", "-b", "spaced", wt)

	list := mustWorktrees(t, repo)
	if len(list) != 2 {
		t.Fatalf("len = %d, want 2: %+v", len(list), list)
	}
	assertSamePath(t, list[1].Path, wt)
	if list[1].Branch != "spaced" {
		t.Errorf("Branch = %q, want spaced", list[1].Branch)
	}
}
