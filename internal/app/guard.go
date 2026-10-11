package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/render"
)

type skippedRepo struct {
	Path   string
	Reason string
	Remote string
	URL    string
}

type guard struct {
	ctx   context.Context
	pairs map[string]matched
}

func newGuard(ctx context.Context, pairs []matched) (*guard, error) {
	version, err := gitcmd.GitVersion(ctx)
	if err != nil {
		return nil, err
	}
	if !version.AtLeast(2, 31) {
		return nil, fmt.Errorf("git 2.31 or newer is required to detect worktrees, found %d.%d", version.Major, version.Minor)
	}
	g := &guard{ctx: ctx, pairs: make(map[string]matched, len(pairs))}
	for _, pair := range pairs {
		g.pairs[pair.local.Path] = pair
	}
	return g, nil
}

func (g *guard) check(path string) (skippedRepo, bool) {
	var reasons []string
	worktrees, err := gitcmd.Worktrees(g.ctx, path)
	if err != nil {
		reasons = append(reasons, "worktrees unreadable: "+err.Error())
	} else if extra := blockingWorktrees(path, worktrees); len(extra) > 0 {
		paths := make([]string, len(extra))
		for i, w := range extra {
			paths[i] = shortPath(w.Path)
		}
		reasons = append(reasons, "worktrees: "+strings.Join(paths, ", "))
	}
	if len(reasons) == 0 {
		return skippedRepo{}, false
	}
	row := skippedRepo{Path: path, Reason: strings.Join(reasons, "; ")}
	if pair, ok := g.pairs[path]; ok {
		row.Remote = pair.gitRemote()
		row.URL = pair.remote.WebURL
	}
	return row, true
}

func printSkipped(p *render.Printer, rows []skippedRepo) {
	panic("not implemented")
}
