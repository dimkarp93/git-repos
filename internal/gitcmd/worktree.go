package gitcmd

import "context"

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
	panic("not implemented")
}

func parseVersion(line string) (Version, error) {
	panic("not implemented")
}

func GitVersion(ctx context.Context) (Version, error) {
	panic("not implemented")
}

func Worktrees(ctx context.Context, dir string) ([]Worktree, error) {
	panic("not implemented")
}
