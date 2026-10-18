package gitcmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

type Worktree struct {
	Path     string
	Head     string
	Branch   string
	Detached bool
	Prunable bool
	Locked   bool
	Bare     bool
}

type Version struct {
	Major int
	Minor int
}

func (v Version) AtLeast(major, minor int) bool {
	if v.Major != major {
		return v.Major > major
	}
	return v.Minor >= minor
}

func parseVersion(line string) (Version, error) {
	fields := strings.Fields(line)
	for i, field := range fields {
		if field != "version" || i+1 >= len(fields) {
			continue
		}
		parts := strings.Split(fields[i+1], ".")
		if len(parts) < 2 {
			break
		}
		major, errMajor := strconv.Atoi(parts[0])
		minor, errMinor := strconv.Atoi(parts[1])
		if errMajor != nil || errMinor != nil {
			break
		}
		return Version{Major: major, Minor: minor}, nil
	}
	return Version{}, fmt.Errorf("git --version: unexpected output %q", line)
}

func GitVersion(ctx context.Context) (Version, error) {
	out, _, err := run(ctx, "", "--version")
	if err != nil {
		return Version{}, err
	}
	return parseVersion(out)
}

func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	out, _, err := run(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	return parseWorktrees(out), nil
}

func parseWorktrees(out string) []Worktree {
	var list []Worktree
	for _, line := range strings.Split(out, "\n") {
		key, value, _ := strings.Cut(strings.TrimRight(line, "\r"), " ")
		switch {
		case key == "worktree":
			list = append(list, Worktree{Path: value})
		case len(list) > 0:
			list[len(list)-1].set(key, value)
		}
	}
	return list
}

func (w *Worktree) set(key, value string) {
	switch key {
	case "HEAD":
		w.Head = value
	case "branch":
		w.Branch = strings.TrimPrefix(value, "refs/heads/")
	case "detached":
		w.Detached = true
	case "prunable":
		w.Prunable = true
	case "locked":
		w.Locked = true
	case "bare":
		w.Bare = true
	}
}
