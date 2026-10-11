package app

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
)

func newTestGuard(t *testing.T, roots ...string) *guard {
	t.Helper()
	prov := &fakeProvider{repos: []provider.Repo{{Owner: "tester", Name: "tool", DefaultBranch: "main"}}}
	pairs, _, err := collect(context.Background(), prov, nil, config.Default(), roots, options{}, &progress{})
	if err != nil {
		t.Fatal(err)
	}
	g, err := newGuard(context.Background(), pairs)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGuardAllowsPlainRepository(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")

	if row, blocked := newTestGuard(t, root).check(dir); blocked {
		t.Fatalf("blocked: %+v", row)
	}
}

func TestGuardBlocksExtraWorktree(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	wt := filepath.Join(t.TempDir(), "feature")
	git(t, dir, "worktree", "add", "-b", "feature", wt)

	row, blocked := newTestGuard(t, root).check(dir)
	if !blocked || !strings.Contains(row.Reason, "worktrees:") || row.Remote != "origin" {
		t.Fatalf("row = %+v, blocked = %v", row, blocked)
	}
}

func TestGuardIgnoresDetachedWorktree(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	wt := filepath.Join(t.TempDir(), "_ide")
	git(t, dir, "worktree", "add", "--detach", wt)

	if row, blocked := newTestGuard(t, root).check(dir); blocked {
		t.Fatalf("blocked: %+v", row)
	}
}

func TestGuardIgnoresPrunableWorktree(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	wt := filepath.Join(t.TempDir(), "gone")
	git(t, dir, "worktree", "add", "-b", "gone", wt)
	git(t, dir, "worktree", "remove", "--force", wt)
	git(t, dir, "worktree", "prune")

	if row, blocked := newTestGuard(t, root).check(dir); blocked {
		t.Fatalf("blocked: %+v", row)
	}
}

func TestGuardCandidatesSplitsBlocked(t *testing.T) {
	root := t.TempDir()
	plain := repoWithOrigin(t, root, "plain", "https://gitlab.example/x/plain.git")
	busy := repoWithOrigin(t, root, "busy", "https://gitlab.example/x/busy.git")
	git(t, busy, "worktree", "add", "-b", "feature", filepath.Join(t.TempDir(), "feature"))

	g, err := newGuard(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	kept, blocked := guardCandidates(g, []LocalOnly{{Path: plain}, {Path: busy}})
	if len(kept) != 1 || kept[0].Path != plain || len(blocked) != 1 || blocked[0].Path != busy {
		t.Fatalf("kept = %+v, blocked = %+v", kept, blocked)
	}
	if exitSkipped(blocked) != ExitFailure || exitSkipped(nil) != ExitOK {
		t.Error("exitSkipped does not follow the skipped rows")
	}
}

func TestPrintSkippedRendersRows(t *testing.T) {
	var out bytes.Buffer
	p := render.NewPrinter(&out, false)
	printSkipped(p, []skippedRepo{{Path: "/src/tool", Reason: "worktrees: /wt/a", Remote: "origin", URL: "https://fake.test/tester/tool"}})
	for _, want := range []string{"/src/tool", "worktrees: /wt/a", "origin", "https://fake.test/tester/tool"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	out.Reset()
	printSkipped(p, nil)
	if out.Len() != 0 {
		t.Errorf("output for no rows: %q", out.String())
	}
}
