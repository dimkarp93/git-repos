package app

import (
	"context"

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
	panic("not implemented")
}

func (g *guard) check(path string) (skippedRepo, bool) {
	panic("not implemented")
}

func printSkipped(p *render.Printer, rows []skippedRepo) {
	panic("not implemented")
}
