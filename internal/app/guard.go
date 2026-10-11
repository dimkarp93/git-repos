package app

import (
	"context"
	"fmt"

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
	panic("not implemented")
}

func printSkipped(p *render.Printer, rows []skippedRepo) {
	panic("not implemented")
}
