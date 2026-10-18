package app

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
)

func newTestGuard(t *testing.T, roots ...string) *guard {
	t.Helper()
	prov := &fakeProvider{repos: []provider.Repo{{Owner: "tester", Name: "tool", DefaultBranch: "main"}}}
	pairs, _, err := collect(context.Background(), prov, nil, config.Default(), roots, options{}, &progress{})
	require.NoError(t, err)
	g, err := newGuard(context.Background(), pairs)
	require.NoError(t, err)
	return g
}

func TestGuardAllowsPlainRepository(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")

	_, blocked := newTestGuard(t, root).check(dir)

	assert.False(t, blocked)
}

func TestGuardBlocksExtraWorktree(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	wt := filepath.Join(t.TempDir(), "feature")
	git(t, dir, "worktree", "add", "-b", "feature", wt)

	row, blocked := newTestGuard(t, root).check(dir)

	t.Run("blocked", func(t *testing.T) {
		assert.True(t, blocked)
	})
	t.Run("reason", func(t *testing.T) {
		assert.Contains(t, row.Reason, "worktrees:")
	})
	t.Run("remote", func(t *testing.T) {
		assert.Equal(t, "origin", row.Remote)
	})
}

func TestGuardIgnoresDetachedWorktree(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	wt := filepath.Join(t.TempDir(), "_ide")
	git(t, dir, "worktree", "add", "--detach", wt)

	_, blocked := newTestGuard(t, root).check(dir)

	assert.False(t, blocked)
}

func TestGuardIgnoresPrunableWorktree(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	wt := filepath.Join(t.TempDir(), "gone")
	git(t, dir, "worktree", "add", "-b", "gone", wt)
	git(t, dir, "worktree", "remove", "--force", wt)
	git(t, dir, "worktree", "prune")

	_, blocked := newTestGuard(t, root).check(dir)

	assert.False(t, blocked)
}

func splitCandidates(t *testing.T) ([]LocalOnly, []skippedRepo, string, string) {
	t.Helper()

	root := t.TempDir()
	plain := repoWithOrigin(t, root, "plain", "https://gitlab.example/x/plain.git")
	busy := repoWithOrigin(t, root, "busy", "https://gitlab.example/x/busy.git")
	git(t, busy, "worktree", "add", "-b", "feature", filepath.Join(t.TempDir(), "feature"))

	g, err := newGuard(context.Background(), nil)
	require.NoError(t, err)
	kept, blocked := guardCandidates(g, []LocalOnly{{Path: plain}, {Path: busy}})
	return kept, blocked, plain, busy
}

func TestGuardCandidatesSplitsBlocked(t *testing.T) {
	kept, blocked, plain, busy := splitCandidates(t)

	t.Run("kept", func(t *testing.T) {
		assert.Equal(t, []LocalOnly{{Path: plain}}, kept)
	})
	t.Run("blocked", func(t *testing.T) {
		require.Len(t, blocked, 1)
		assert.Equal(t, busy, blocked[0].Path)
	})
}

func TestExitSkipped(t *testing.T) {
	t.Run("with skipped rows", func(t *testing.T) {
		assert.Equal(t, ExitFailure, exitSkipped([]skippedRepo{{Path: "/x"}}))
	})
	t.Run("without rows", func(t *testing.T) {
		assert.Equal(t, ExitOK, exitSkipped(nil))
	})
}

func TestPrintSkippedRendersRows(t *testing.T) {
	var out bytes.Buffer

	printSkipped(render.NewPrinter(&out, false), []skippedRepo{{Path: "/src/tool", Reason: "worktrees: /wt/a", Remote: "origin", URL: "https://fake.test/tester/tool"}})

	for _, want := range []string{"/src/tool", "worktrees: /wt/a", "origin", "https://fake.test/tester/tool"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out.String(), want)
		})
	}
}

func TestPrintSkippedWithoutRows(t *testing.T) {
	var out bytes.Buffer

	printSkipped(render.NewPrinter(&out, false), nil)

	assert.Empty(t, out.String())
}
