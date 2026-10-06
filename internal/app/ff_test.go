package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimkarp93/git-repos/internal/render"
)

func ffSetup(t *testing.T) (local, other string) {
	t.Helper()
	root := t.TempDir()
	seed := filepath.Join(root, "seed")
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
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

func TestFastForwardCheckedOutBranch(t *testing.T) {
	local, other := ffSetup(t)
	head := advanceOrigin(t, local, other, "b.txt")

	res := fastForward(context.Background(), local, "origin", "main")
	if res.Outcome != ffForwarded || res.Behind != 1 || !res.CheckedOut {
		t.Fatalf("res = %+v", res)
	}
	if got := git(t, local, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD = %s, want %s", got, head)
	}
	if _, err := os.Stat(filepath.Join(local, "b.txt")); err != nil {
		t.Errorf("working tree not updated: %v", err)
	}
}

func TestFastForwardBranchNotCheckedOut(t *testing.T) {
	local, other := ffSetup(t)
	git(t, local, "checkout", "--quiet", "-b", "feature")
	head := advanceOrigin(t, local, other, "b.txt")

	res := fastForward(context.Background(), local, "origin", "main")
	if res.Outcome != ffForwarded || res.CheckedOut {
		t.Fatalf("res = %+v", res)
	}
	if got := git(t, local, "rev-parse", "refs/heads/main"); got != head {
		t.Errorf("main = %s, want %s", got, head)
	}
	if got := git(t, local, "symbolic-ref", "--short", "HEAD"); got != "feature" {
		t.Errorf("current branch = %s", got)
	}
}

func TestFastForwardUpToDate(t *testing.T) {
	local, _ := ffSetup(t)
	if res := fastForward(context.Background(), local, "origin", "main"); res.Outcome != ffUpToDate {
		t.Fatalf("res = %+v", res)
	}
}

func TestFastForwardAhead(t *testing.T) {
	local, _ := ffSetup(t)
	head := commit(t, local, "b.txt")

	res := fastForward(context.Background(), local, "origin", "main")
	if res.Outcome != ffAhead || res.Ahead != 1 {
		t.Fatalf("res = %+v", res)
	}
	if got := git(t, local, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved to %s", got)
	}
}

func TestFastForwardDivergedIsConflict(t *testing.T) {
	local, other := ffSetup(t)
	head := commit(t, local, "local.txt")
	remote := advanceOrigin(t, local, other, "remote.txt")

	res := fastForward(context.Background(), local, "origin", "main")
	if res.Outcome != ffConflict || res.Ahead != 1 || res.Behind != 1 {
		t.Fatalf("res = %+v", res)
	}
	if res.LocalSHA != head || res.RemoteSHA != remote {
		t.Errorf("shas = %s %s", res.LocalSHA, res.RemoteSHA)
	}
	if got := git(t, local, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved to %s", got)
	}
}

func TestFastForwardRefusesToOverwriteLocalFiles(t *testing.T) {
	local, other := ffSetup(t)
	before := git(t, local, "rev-parse", "HEAD")
	advanceOrigin(t, local, other, "b.txt")
	if err := os.WriteFile(filepath.Join(local, "b.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := fastForward(context.Background(), local, "origin", "main")
	if res.Outcome != ffError {
		t.Fatalf("res = %+v", res)
	}
	if got := git(t, local, "rev-parse", "HEAD"); got != before {
		t.Errorf("HEAD moved to %s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(local, "b.txt")); string(data) != "mine" {
		t.Errorf("local file overwritten: %q", data)
	}
}

func TestFastForwardNeedsFetchedOrigin(t *testing.T) {
	local, _ := ffSetup(t)
	git(t, local, "update-ref", "-d", "refs/remotes/origin/main")

	res := fastForward(context.Background(), local, "origin", "main")
	if res.Outcome != ffError || !strings.Contains(res.Detail, "run update") {
		t.Fatalf("res = %+v", res)
	}
}

func TestFastForwardNoLocalBranch(t *testing.T) {
	local, _ := ffSetup(t)
	if res := fastForward(context.Background(), local, "origin", "develop"); res.Outcome != ffSkipped {
		t.Fatalf("res = %+v", res)
	}
}

func TestPrintConflictSuggestsCommands(t *testing.T) {
	local := strings.Repeat("a", 40)
	remote := strings.Repeat("b", 40)
	base := ffResult{
		FullName: "tester/repo", Path: "~/repo", dir: "/home/u/my repo", Branch: "main",
		Outcome: ffConflict, LocalSHA: local, RemoteSHA: remote, Ahead: 2, Behind: 3,
	}

	checkedOut := base
	checkedOut.CheckedOut = true
	var out bytes.Buffer
	code := printFF(render.NewPrinter(&out, false), []ffResult{checkedOut})
	text := out.String()
	if code != ExitFailure {
		t.Errorf("exit code = %d", code)
	}
	for _, want := range []string{
		"Version conflict: tester/repo (main)",
		"git -C '/home/u/my repo' merge origin/main",
		"git -C '/home/u/my repo' push origin main",
		"push --force-with-lease=main:" + remote + " origin main",
		"branch backup/main-aaaaaaa main",
		"reset --hard origin/main",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "checkout main") || strings.Contains(text, "branch -f") {
		t.Errorf("unexpected commands for checked out branch:\n%s", text)
	}

	out.Reset()
	printFF(render.NewPrinter(&out, false), []ffResult{base})
	text = out.String()
	for _, want := range []string{"checkout main", "branch -f main origin/main"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "reset --hard") {
		t.Errorf("reset --hard suggested for a branch that is not checked out:\n%s", text)
	}
}

func TestPrintFFSucceedsWithoutConflicts(t *testing.T) {
	var out bytes.Buffer
	results := []ffResult{
		{FullName: "tester/a", Branch: "main", Outcome: ffForwarded, Behind: 1},
		{FullName: "tester/b", Branch: "main", Outcome: ffAhead, Ahead: 1},
		{FullName: "tester/c", Branch: "main", Outcome: ffUpToDate},
	}
	if code := printFF(render.NewPrinter(&out, false), results); code != ExitOK {
		t.Errorf("exit code = %d", code)
	}
	if strings.Contains(out.String(), "Version conflict") {
		t.Errorf("unexpected conflict block:\n%s", out.String())
	}
}
