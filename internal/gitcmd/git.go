package gitcmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func run(ctx context.Context, dir string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := strings.TrimSpace(stdout.String())
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out, exitErr.ExitCode(), fmt.Errorf("git %s: %s", strings.Join(args, " "), firstLine(stderr.String()))
		}
		return out, -1, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, 0, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "failed"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func OriginURL(ctx context.Context, dir string) (string, error) {
	out, code, err := run(ctx, dir, "config", "--get", "remote.origin.url")
	if code == 1 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return out, nil
}

func RevParse(ctx context.Context, dir, rev string) (string, error) {
	out, code, err := run(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if code == 1 || (err == nil && out == "") {
		return "", ErrNoRef
	}
	if err != nil {
		return "", err
	}
	return out, nil
}

var ErrNoRef = errors.New("ref not found")

func HasCommit(ctx context.Context, dir, sha string) bool {
	_, code, _ := run(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return code == 0
}

func IsAncestor(ctx context.Context, dir, ancestor, descendant string) (bool, error) {
	_, code, err := run(ctx, dir, "merge-base", "--is-ancestor", ancestor, descendant)
	switch code {
	case 0:
		return true, nil
	case 1:
		return false, nil
	default:
		return false, err
	}
}

func Fetch(ctx context.Context, dir, remote, branch string) error {
	args := []string{"fetch", "--quiet", "--no-tags", remote}
	if branch != "" {
		args = append(args, branch)
	}
	_, _, err := run(ctx, dir, args...)
	return err
}

func MergeFFOnly(ctx context.Context, dir, ref string) error {
	_, _, err := run(ctx, dir, "merge", "--ff-only", "--quiet", ref)
	return err
}

func FastForwardBranch(ctx context.Context, dir, remote, branch string) error {
	_, _, err := run(ctx, dir, "fetch", "--quiet", ".", "refs/remotes/"+remote+"/"+branch+":refs/heads/"+branch)
	return err
}

func CountCommits(ctx context.Context, dir, from, to string) (int, error) {
	out, _, err := run(ctx, dir, "rev-list", "--count", from+".."+to)
	if err != nil {
		return 0, err
	}
	var n int
	if _, err := fmt.Sscanf(out, "%d", &n); err != nil {
		return 0, fmt.Errorf("git rev-list --count: %q: %w", out, err)
	}
	return n, nil
}

func GitDir(ctx context.Context, dir string) (string, error) {
	out, _, err := run(ctx, dir, "rev-parse", "--absolute-git-dir")
	return out, err
}

func LastFetch(ctx context.Context, dir string) (time.Time, bool) {
	gitDir, err := GitDir(ctx, dir)
	if err != nil {
		return time.Time{}, false
	}
	candidates := []string{
		filepath.Join(gitDir, "FETCH_HEAD"),
		filepath.Join(gitDir, "refs", "remotes", "origin"),
		filepath.Join(gitDir, "packed-refs"),
	}
	var newest time.Time
	for _, path := range candidates {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	if newest.IsZero() {
		return time.Time{}, false
	}
	return newest, true
}

func CurrentBranch(ctx context.Context, dir string) (string, error) {
	out, _, err := run(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return out, nil
}

func HasCommits(ctx context.Context, dir string) bool {
	_, code, _ := run(ctx, dir, "rev-parse", "--verify", "--quiet", "HEAD")
	return code == 0
}

func IsDirty(ctx context.Context, dir string) (bool, error) {
	out, _, err := run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func SetRemote(ctx context.Context, dir, remote, url string) error {
	if _, code, _ := run(ctx, dir, "remote", "get-url", remote); code == 0 {
		_, _, err := run(ctx, dir, "remote", "set-url", remote, url)
		return err
	}
	_, _, err := run(ctx, dir, "remote", "add", remote, url)
	return err
}

func Push(ctx context.Context, dir, remote, branch string) error {
	_, _, err := run(ctx, dir, "push", "--quiet", "--set-upstream", remote, branch)
	return err
}

func Clone(ctx context.Context, parent, url, name, branch string) error {
	args := []string{"clone", "--quiet"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, url, name)
	_, _, err := run(ctx, parent, args...)
	return err
}

func DefaultBranch(ctx context.Context, dir string) string {
	if out, _, err := run(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		if _, name, ok := strings.Cut(out, "/"); ok && name != "" {
			return name
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, code, _ := run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+name); code == 0 {
			return name
		}
	}
	return ""
}

func HasRemoteRefs(ctx context.Context, dir, remote string) bool {
	out, _, err := run(ctx, dir, "for-each-ref", "--count=1", "refs/remotes/"+remote)
	return err == nil && out != ""
}
