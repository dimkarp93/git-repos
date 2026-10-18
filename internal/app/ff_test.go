package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/render"
)

func ffSetup(t *testing.T) (local, other string) {
	t.Helper()

	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	require.NoError(t, os.MkdirAll(seed, 0o755))
	git(t, seed, "init", "--initial-branch=main", "--quiet")
	commit(t, seed, "a.txt")

	origin := filepath.Join(root, "origin.git")
	git(t, root, "clone", "--quiet", "--bare", seed, origin)
	local = filepath.Join(root, "local")
	other = filepath.Join(root, "other")
	git(t, root, "clone", "--quiet", origin, local)
	git(t, root, "clone", "--quiet", origin, other)
	return local, other
}

func advanceOrigin(t *testing.T, local, other, name string) string {
	t.Helper()
	head := commit(t, other, name)
	git(t, other, "push", "--quiet", "origin", "main")
	git(t, local, "fetch", "--quiet", "origin")
	return head
}

type ffState struct {
	Outcome    ffOutcome
	Ahead      int
	Behind     int
	CheckedOut bool
}

func stateOf(res ffResult) ffState {
	return ffState{Outcome: res.Outcome, Ahead: res.Ahead, Behind: res.Behind, CheckedOut: res.CheckedOut}
}

func TestFastForwardCheckedOutBranch(t *testing.T) {
	local, other := ffSetup(t)
	head := advanceOrigin(t, local, other, "b.txt")

	res := fastForward(context.Background(), local, "origin", "main")

	t.Run("result", func(t *testing.T) {
		assert.Equal(t, ffState{Outcome: ffForwarded, Behind: 1, CheckedOut: true}, stateOf(res))
	})
	t.Run("head moved", func(t *testing.T) {
		assert.Equal(t, head, git(t, local, "rev-parse", "HEAD"))
	})
	t.Run("working tree updated", func(t *testing.T) {
		assert.FileExists(t, filepath.Join(local, "b.txt"))
	})
}

func TestFastForwardBranchNotCheckedOut(t *testing.T) {
	local, other := ffSetup(t)
	git(t, local, "checkout", "--quiet", "-b", "feature")
	head := advanceOrigin(t, local, other, "b.txt")

	res := fastForward(context.Background(), local, "origin", "main")

	t.Run("result", func(t *testing.T) {
		assert.Equal(t, ffState{Outcome: ffForwarded, Behind: 1}, stateOf(res))
	})
	t.Run("branch moved", func(t *testing.T) {
		assert.Equal(t, head, git(t, local, "rev-parse", "refs/heads/main"))
	})
	t.Run("checkout kept", func(t *testing.T) {
		assert.Equal(t, "feature", git(t, local, "symbolic-ref", "--short", "HEAD"))
	})
}

func TestFastForwardUpToDate(t *testing.T) {
	local, _ := ffSetup(t)

	res := fastForward(context.Background(), local, "origin", "main")

	assert.Equal(t, ffUpToDate, res.Outcome)
}

func TestFastForwardAhead(t *testing.T) {
	local, _ := ffSetup(t)
	head := commit(t, local, "b.txt")

	res := fastForward(context.Background(), local, "origin", "main")

	t.Run("result", func(t *testing.T) {
		assert.Equal(t, ffState{Outcome: ffAhead, Ahead: 1, CheckedOut: true}, stateOf(res))
	})
	t.Run("head kept", func(t *testing.T) {
		assert.Equal(t, head, git(t, local, "rev-parse", "HEAD"))
	})
}

func TestFastForwardDivergedIsConflict(t *testing.T) {
	local, other := ffSetup(t)
	head := commit(t, local, "local.txt")
	remote := advanceOrigin(t, local, other, "remote.txt")

	res := fastForward(context.Background(), local, "origin", "main")

	t.Run("result", func(t *testing.T) {
		assert.Equal(t, ffState{Outcome: ffConflict, Ahead: 1, Behind: 1, CheckedOut: true}, stateOf(res))
	})
	t.Run("shas", func(t *testing.T) {
		assert.Equal(t, []string{head, remote}, []string{res.LocalSHA, res.RemoteSHA})
	})
	t.Run("head kept", func(t *testing.T) {
		assert.Equal(t, head, git(t, local, "rev-parse", "HEAD"))
	})
}

func untrackedConflict(t *testing.T) (string, string) {
	t.Helper()

	local, other := ffSetup(t)
	before := git(t, local, "rev-parse", "HEAD")
	advanceOrigin(t, local, other, "b.txt")
	require.NoError(t, os.WriteFile(filepath.Join(local, "b.txt"), []byte("mine"), 0o644))

	return local, before
}

func TestFastForwardRefusesToOverwriteLocalFiles(t *testing.T) {
	local, before := untrackedConflict(t)

	res := fastForward(context.Background(), local, "origin", "main")

	t.Run("error", func(t *testing.T) {
		assert.Equal(t, ffError, res.Outcome)
	})
	t.Run("head kept", func(t *testing.T) {
		assert.Equal(t, before, git(t, local, "rev-parse", "HEAD"))
	})
	t.Run("local file kept", func(t *testing.T) {
		data, _ := os.ReadFile(filepath.Join(local, "b.txt"))
		assert.Equal(t, "mine", string(data))
	})
}

func TestFastForwardNeedsFetchedOrigin(t *testing.T) {
	local, _ := ffSetup(t)
	git(t, local, "update-ref", "-d", "refs/remotes/origin/main")

	res := fastForward(context.Background(), local, "origin", "main")

	assert.Equal(t, []any{ffError, true}, []any{res.Outcome, strings.Contains(res.Detail, "run update")})
}

func TestFastForwardNoLocalBranch(t *testing.T) {
	local, _ := ffSetup(t)

	res := fastForward(context.Background(), local, "origin", "develop")

	assert.Equal(t, ffSkipped, res.Outcome)
}

func TestCompareDefaultUnknownBranch(t *testing.T) {
	res := compareDefault(context.Background(), t.TempDir(), "origin", "")

	assert.Equal(t, ffError, res.Outcome)
}

func conflictResult(checkedOut bool) ffResult {
	return ffResult{
		FullName: "tester/repo", Path: "~/repo", dir: "/home/u/my repo", Branch: "main",
		Outcome: ffConflict, LocalSHA: strings.Repeat("a", 40), RemoteSHA: strings.Repeat("b", 40),
		Ahead: 2, Behind: 3, CheckedOut: checkedOut,
	}
}

func printedFF(results ...ffResult) (string, int) {
	var out bytes.Buffer
	code := printFF(render.NewPrinter(&out, false), results)
	return out.String(), code
}

func TestPrintConflictForCheckedOutBranch(t *testing.T) {
	text, code := printedFF(conflictResult(true))

	t.Run("exit code", func(t *testing.T) {
		assert.Equal(t, ExitFailure, code)
	})
	for _, want := range []string{
		"Version conflict: tester/repo (main)",
		"git -C '/home/u/my repo' merge origin/main",
		"git -C '/home/u/my repo' push origin main",
		"push --force-with-lease=main:" + strings.Repeat("b", 40) + " origin main",
		"branch backup/main-aaaaaaa main",
		"reset --hard origin/main",
	} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, text, want)
		})
	}
	for _, unwanted := range []string{"checkout main", "branch -f"} {
		t.Run("no "+unwanted, func(t *testing.T) {
			assert.NotContains(t, text, unwanted)
		})
	}
}

func TestPrintConflictForOtherBranch(t *testing.T) {
	text, _ := printedFF(conflictResult(false))

	for _, want := range []string{"checkout main", "branch -f main origin/main"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, text, want)
		})
	}
	t.Run("no reset", func(t *testing.T) {
		assert.NotContains(t, text, "reset --hard")
	})
}

func TestPrintFFSucceedsWithoutConflicts(t *testing.T) {
	text, code := printedFF(
		ffResult{FullName: "tester/a", Branch: "main", Outcome: ffForwarded, Behind: 1},
		ffResult{FullName: "tester/b", Branch: "main", Outcome: ffAhead, Ahead: 1},
		ffResult{FullName: "tester/c", Branch: "main", Outcome: ffUpToDate},
	)

	t.Run("exit code", func(t *testing.T) {
		assert.Equal(t, ExitOK, code)
	})
	t.Run("no conflict block", func(t *testing.T) {
		assert.NotContains(t, text, "Version conflict")
	})
}

func TestPushedText(t *testing.T) {
	cases := map[string]ffResult{
		"pushed +2":               {Ahead: 2, RemoteSHA: "x"},
		"pushed":                  {Ahead: 2},
		"pushed +1, upstream set": {Ahead: 1, RemoteSHA: "x", UpstreamSet: true},
	}
	for want, res := range cases {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, pushedText(res))
		})
	}
}
