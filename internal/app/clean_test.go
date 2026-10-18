package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/provider"
)

func makeDirs(t *testing.T, root string, names ...string) []LocalOnly {
	t.Helper()
	items := make([]LocalOnly, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(filepath.Join(path, ".git"), 0o755))
		items = append(items, LocalOnly{Path: path, Kind: KindNoOrigin, Reason: "no origin", Name: name})
	}
	return items
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

type cleanResult struct {
	deleted, failed int
	exists          []bool
	out             string
}

func cleanLocalWith(t *testing.T, answers string) cleanResult {
	t.Helper()

	root := t.TempDir()
	items := makeDirs(t, root, "a", "b", "c")
	var out bytes.Buffer
	s := newSession(t, bareProvider(t), []string{root}, &out)

	deleted, failed := s.cleanLocal(items, newScriptedConfirmer(strings.NewReader(answers), &out, false))

	res := cleanResult{deleted: deleted, failed: failed, out: out.String()}
	for _, item := range items {
		res.exists = append(res.exists, exists(item.Path))
	}
	return res
}

func TestCleanLocalAnswers(t *testing.T) {
	cases := map[string]struct {
		answers string
		want    cleanResult
	}{
		"per repository": {answers: "yes\nskip\nyes\n", want: cleanResult{deleted: 2, exists: []bool{false, true, false}}},
		"skip-to-all":    {answers: "yes\nskip-to-all\nyes\n", want: cleanResult{deleted: 1, exists: []bool{false, true, true}}},
		"yes-to-all":     {answers: "yes-to-all\n", want: cleanResult{deleted: 3, exists: []bool{false, false, false}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := cleanLocalWith(t, tc.answers)
			got.out = ""
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCleanLocalWarnsAboutDirtyTree(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dirty")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	git(t, dir, "init", "--initial-branch=main", "--quiet")
	commit(t, dir, "a.txt")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644))
	var out bytes.Buffer
	s := newSession(t, bareProvider(t), []string{root}, &out)

	s.cleanLocal([]LocalOnly{{Path: dir, Kind: KindNoOrigin, Reason: "no origin"}}, newScriptedConfirmer(strings.NewReader("skip\n"), &out, false))

	assert.Contains(t, out.String(), "uncommitted changes")
}

func remoteItems(names ...string) []RemoteOnly {
	items := make([]RemoteOnly, 0, len(names))
	for _, name := range names {
		items = append(items, RemoteOnly{FullName: "tester/" + name, repo: provider.Repo{Owner: "tester", Name: name}})
	}
	return items
}

func cleanRemoteWith(t *testing.T, prov *fakeProvider, answers string, names ...string) cleanResult {
	t.Helper()

	var out bytes.Buffer
	s := newSession(t, prov, []string{t.TempDir()}, &out)

	deleted, failed := s.cleanRemote(remoteItems(names...), newScriptedConfirmer(strings.NewReader(answers), &out, false))
	return cleanResult{deleted: deleted, failed: failed, out: out.String()}
}

func TestCleanRemoteAnswers(t *testing.T) {
	prov := bareProvider(t)
	res := cleanRemoteWith(t, prov, "skip\nyes\nyes\n", "a", "b", "c")

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, []int{2, 0}, []int{res.deleted, res.failed})
	})
	t.Run("deleted repositories", func(t *testing.T) {
		assert.Equal(t, []string{"tester/b", "tester/c"}, prov.deleted)
	})
}

func TestCleanRemoteCountsErrors(t *testing.T) {
	prov := bareProvider(t)
	prov.failOn = map[string]error{"delete:tester/a": errors.New("no delete_repo scope")}
	res := cleanRemoteWith(t, prov, "yes\n", "a")

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, []int{0, 1}, []int{res.deleted, res.failed})
	})
	t.Run("reports error", func(t *testing.T) {
		assert.Contains(t, res.out, "no delete_repo scope")
	})
}

func TestCleanRemoteYesToAll(t *testing.T) {
	prov := bareProvider(t)
	res := cleanRemoteWith(t, prov, "yes-to-all\n", "a", "b")

	t.Run("count", func(t *testing.T) {
		assert.Equal(t, 2, res.deleted)
	})
	t.Run("deleted repositories", func(t *testing.T) {
		assert.Len(t, prov.deleted, 2)
	})
}

func TestRemoteNote(t *testing.T) {
	item := RemoteOnly{Archived: true, repo: provider.Repo{Private: true}}

	assert.Equal(t, "\n  the repository is archived\n  the repository is private", remoteNote(item))
}
