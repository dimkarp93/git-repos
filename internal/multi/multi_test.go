package multi

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	data := []byte(`[
  {"name": "main", "kind": "main", "path": "/src/app", "branch": "main", "dirty": true, "ahead": 0, "behind": 0},
  {"name": "feat", "kind": "real", "path": "/wt/feat/src/app", "branch": "feat", "dirty": false, "ahead": 2, "behind": 0},
  {"name": "gone", "kind": "real", "path": "/wt/gone/src/app", "dirty": false, "ahead": 0, "behind": 0, "missing": true}
]`)

	list, err := Parse(data)

	require.NoError(t, err)
	assert.Equal(t, []Workspace{
		{Name: "main", Kind: Main, Path: "/src/app", Branch: "main", Dirty: true},
		{Name: "feat", Kind: Real, Path: "/wt/feat/src/app", Branch: "feat"},
		{Name: "gone", Kind: Real, Path: "/wt/gone/src/app", Missing: true},
	}, list)
}

func TestParseRejectsGarbage(t *testing.T) {
	_, err := Parse([]byte("not json"))

	assert.Error(t, err)
}

func TestReasonSkipsWarnings(t *testing.T) {
	stderr := "git-multi: warning: the main checkout is on \"x\", expected the default branch \"main\"\ngit-multi: no origin/HEAD\n"

	assert.Equal(t, "no origin/HEAD", reason(stderr, errors.New("exit status 2")))
}

func TestReasonFallsBackToError(t *testing.T) {
	assert.Equal(t, "exit status 2", reason("", errors.New("exit status 2")))
}

func TestCheckWithoutBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	assert.ErrorIs(t, Check(), ErrNotInstalled)
}
