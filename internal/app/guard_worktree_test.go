package app

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
)

func TestBlockingWorktrees(t *testing.T) {
	main := gitcmd.Worktree{Path: "/src/tool", Branch: "main"}
	feature := gitcmd.Worktree{Path: "/wt/tool/feature", Branch: "feature"}
	detached := gitcmd.Worktree{Path: "/tmp/git-multi/tool/_ide", Detached: true}
	onBranch := gitcmd.Worktree{Path: "/wt/tool/_odd", Branch: "odd"}
	gone := gitcmd.Worktree{Path: "/wt/tool/gone", Branch: "gone", Prunable: true}
	bare := gitcmd.Worktree{Path: "/srv/tool.git", Bare: true}

	cases := []struct {
		name string
		dir  string
		all  []gitcmd.Worktree
		want []gitcmd.Worktree
	}{
		{name: "only main", dir: "/src/tool", all: []gitcmd.Worktree{main}},
		{name: "detached ignored", dir: "/src/tool", all: []gitcmd.Worktree{main, detached}},
		{name: "prunable ignored", dir: "/src/tool", all: []gitcmd.Worktree{main, gone}},
		{name: "bare ignored", dir: "/src/tool", all: []gitcmd.Worktree{bare, main}},
		{name: "worktree on a branch blocks", dir: "/src/tool", all: []gitcmd.Worktree{main, feature, detached}, want: []gitcmd.Worktree{feature}},
		{name: "name does not matter", dir: "/src/tool", all: []gitcmd.Worktree{main, onBranch}, want: []gitcmd.Worktree{onBranch}},
		{name: "linked worktree is blocked by main", dir: "/wt/tool/feature", all: []gitcmd.Worktree{main, feature}, want: []gitcmd.Worktree{main}},
		{name: "path is cleaned", dir: "/src/tool/", all: []gitcmd.Worktree{main, feature}, want: []gitcmd.Worktree{feature}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := blockingWorktrees(tc.dir, tc.all)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			assert.True(t, reflect.DeepEqual(got, tc.want), "blockingWorktrees = %+v, want %+v", got, tc.want)
		})
	}
}
