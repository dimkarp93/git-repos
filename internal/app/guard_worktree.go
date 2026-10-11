package app

import (
	"path/filepath"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
)

func blockingWorktrees(dir string, all []gitcmd.Worktree) []gitcmd.Worktree {
	dir = filepath.Clean(dir)
	var out []gitcmd.Worktree
	for _, w := range all {
		switch {
		case filepath.Clean(w.Path) == dir:
		case w.Prunable, w.Bare:
		case w.Detached:
		default:
			out = append(out, w)
		}
	}
	return out
}
