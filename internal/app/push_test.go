package app

import (
	"context"
	"path/filepath"
	"testing"
)

func originOf(t *testing.T, local string) string {
	t.Helper()
	return git(t, local, "config", "--get", "remote.origin.url")
}

func TestPushDefaultPushesAheadBranch(t *testing.T) {
	local, _ := ffSetup(t)
	head := commit(t, local, "b.txt")

	res := pushDefault(context.Background(), local, "origin", "main")
	if res.Outcome != ffPushed || res.Ahead != 1 || res.UpstreamSet {
		t.Fatalf("res = %+v", res)
	}
	if got := git(t, originOf(t, local), "rev-parse", "main"); got != head {
		t.Errorf("origin main = %s, want %s", got, head)
	}
}

func TestPushDefaultSetsMissingUpstream(t *testing.T) {
	local, _ := ffSetup(t)
	git(t, local, "branch", "--unset-upstream", "main")
	head := commit(t, local, "b.txt")

	res := pushDefault(context.Background(), local, "origin", "main")
	if res.Outcome != ffPushed || !res.UpstreamSet {
		t.Fatalf("res = %+v", res)
	}
	if got := git(t, local, "rev-parse", "--abbrev-ref", "main@{upstream}"); got != "origin/main" {
		t.Errorf("upstream = %s", got)
	}
	if got := git(t, originOf(t, local), "rev-parse", "main"); got != head {
		t.Errorf("origin main = %s, want %s", got, head)
	}
}

func TestPushDefaultCreatesMissingRemoteBranch(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	git(t, root, "init", "--quiet", "--bare", "--initial-branch=main", origin)
	local := filepath.Join(root, "local")
	git(t, root, "init", "--quiet", "--initial-branch=main", local)
	git(t, local, "remote", "add", "origin", origin)
	head := commit(t, local, "a.txt")

	res := pushDefault(context.Background(), local, "origin", "main")
	if res.Outcome != ffPushed || !res.UpstreamSet {
		t.Fatalf("res = %+v", res)
	}
	if got := git(t, origin, "rev-parse", "main"); got != head {
		t.Errorf("origin main = %s, want %s", got, head)
	}
}

func TestPushDefaultLeavesOtherStatesAlone(t *testing.T) {
	local, other := ffSetup(t)
	if res := pushDefault(context.Background(), local, "origin", "main"); res.Outcome != ffUpToDate {
		t.Fatalf("up to date: res = %+v", res)
	}

	remote := advanceOrigin(t, local, other, "b.txt")
	if res := pushDefault(context.Background(), local, "origin", "main"); res.Outcome != ffBehind || res.Behind != 1 {
		t.Fatalf("behind: res = %+v", res)
	}

	commit(t, local, "local.txt")
	res := pushDefault(context.Background(), local, "origin", "main")
	if res.Outcome != ffConflict {
		t.Fatalf("diverged: res = %+v", res)
	}
	if got := git(t, originOf(t, local), "rev-parse", "main"); got != remote {
		t.Errorf("origin main moved to %s", got)
	}
}
