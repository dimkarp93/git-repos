package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func originOf(t *testing.T, local string) string {
	t.Helper()
	return git(t, local, "config", "--get", "remote.origin.url")
}

type ffView struct {
	Outcome     ffOutcome
	Ahead       int
	Behind      int
	UpstreamSet bool
}

func viewFF(res ffResult) ffView {
	return ffView{Outcome: res.Outcome, Ahead: res.Ahead, Behind: res.Behind, UpstreamSet: res.UpstreamSet}
}

func TestPushDefaultPushesAheadBranch(t *testing.T) {
	local, _ := ffSetup(t)
	head := commit(t, local, "b.txt")

	res := pushDefault(context.Background(), local, "origin", "main")

	t.Run("result", func(t *testing.T) {
		assert.Equal(t, ffView{Outcome: ffPushed, Ahead: 1}, viewFF(res))
	})
	t.Run("remote moved", func(t *testing.T) {
		assert.Equal(t, head, git(t, originOf(t, local), "rev-parse", "main"))
	})
}

func TestPushDefaultSetsMissingUpstream(t *testing.T) {
	local, _ := ffSetup(t)
	git(t, local, "branch", "--unset-upstream", "main")
	head := commit(t, local, "b.txt")

	res := pushDefault(context.Background(), local, "origin", "main")

	t.Run("result", func(t *testing.T) {
		assert.Equal(t, ffView{Outcome: ffPushed, Ahead: 1, UpstreamSet: true}, viewFF(res))
	})
	t.Run("upstream", func(t *testing.T) {
		assert.Equal(t, "origin/main", git(t, local, "rev-parse", "--abbrev-ref", "main@{upstream}"))
	})
	t.Run("remote moved", func(t *testing.T) {
		assert.Equal(t, head, git(t, originOf(t, local), "rev-parse", "main"))
	})
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

	t.Run("result", func(t *testing.T) {
		assert.Equal(t, []any{ffPushed, true}, []any{res.Outcome, res.UpstreamSet})
	})
	t.Run("remote created", func(t *testing.T) {
		assert.Equal(t, head, git(t, origin, "rev-parse", "main"))
	})
}

func TestPushDefaultUpToDate(t *testing.T) {
	local, _ := ffSetup(t)

	res := pushDefault(context.Background(), local, "origin", "main")

	assert.Equal(t, ffUpToDate, res.Outcome)
}

func TestPushDefaultLeavesBehindAlone(t *testing.T) {
	local, other := ffSetup(t)
	advanceOrigin(t, local, other, "b.txt")

	res := pushDefault(context.Background(), local, "origin", "main")

	assert.Equal(t, ffView{Outcome: ffBehind, Behind: 1}, viewFF(res))
}

func TestPushDefaultLeavesConflictAlone(t *testing.T) {
	local, other := ffSetup(t)
	remote := advanceOrigin(t, local, other, "b.txt")
	commit(t, local, "local.txt")

	res := pushDefault(context.Background(), local, "origin", "main")

	t.Run("conflict", func(t *testing.T) {
		assert.Equal(t, ffConflict, res.Outcome)
	})
	t.Run("remote untouched", func(t *testing.T) {
		assert.Equal(t, remote, git(t, originOf(t, local), "rev-parse", "main"))
	})
}
