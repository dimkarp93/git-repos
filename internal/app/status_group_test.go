package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func groupResults() []statusResult {
	return []statusResult{
		{repo: "/src/lib", name: "lib", workspace: "main", branch: "main", dirty: true},
		{repo: "/src/app", name: "app", workspace: "main", branch: "main"},
		{repo: "/src/app", name: "app", workspace: "feat", branch: "feat", feature: true, real: true},
		{repo: "/other/app", name: "app", workspace: "main", branch: "fix", feature: true},
		{repo: "/src/broken", name: "broken", err: errors.New("boom")},
	}
}

func mustGroup(t *testing.T, name string) statusGroup {
	t.Helper()
	g, err := lookupStatusGroup(name)
	require.NoError(t, err)
	return g
}

func TestCountGroupsByRepo(t *testing.T) {
	counts := countGroups(groupResults(), filters{}, mustGroup(t, "repo"))

	assert.Equal(t, []groupCount{
		{id: "/other/app", label: "app", count: 1},
		{id: "/src/app", label: "app", count: 2},
		{id: "/src/broken", label: "broken", count: 1},
		{id: "/src/lib", label: "lib", count: 1},
	}, counts)
}

func TestCountGroupsByBranch(t *testing.T) {
	counts := countGroups(groupResults(), filters{}, mustGroup(t, "branch"))

	assert.Equal(t, []groupCount{
		{id: "", label: "", count: 1},
		{id: "feat", label: "feat", count: 1},
		{id: "fix", label: "fix", count: 1},
		{id: "main", label: "main", count: 2},
	}, counts)
}

func TestCountGroupsByWorktree(t *testing.T) {
	counts := countGroups(groupResults(), filters{}, mustGroup(t, "worktree"))

	assert.Equal(t, []groupCount{
		{id: "", label: "", count: 1},
		{id: "feat", label: "feat", count: 1},
		{id: "main", label: "main", count: 3},
	}, counts)
}

func TestCountGroupsAppliesFilters(t *testing.T) {
	counts := countGroups(groupResults(), filters{feature: true}, mustGroup(t, "worktree"))

	assert.Equal(t, []groupCount{
		{id: "feat", label: "feat", count: 1},
		{id: "main", label: "main", count: 1},
	}, counts)
}

func TestGroupRows(t *testing.T) {
	rows := groupRows([]groupCount{{id: "", label: "", count: 1}, {id: "main", label: "main", count: 3}})

	require.Len(t, rows, 2)
	assert.Equal(t, "-", rows[0][0].Text)
	assert.Equal(t, "main", rows[1][0].Text)
	assert.Equal(t, "3", rows[1][1].Text)
}

func TestLookupStatusGroupRejectsUnknown(t *testing.T) {
	_, err := lookupStatusGroup("path")

	assert.EqualError(t, err, "--group accepts repo, branch, worktree")
}
