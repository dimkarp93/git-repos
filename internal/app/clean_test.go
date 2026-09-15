package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimkarp93/git-repos/internal/provider"
)

func makeDirs(t *testing.T, root string, names ...string) []LocalOnly {
	t.Helper()
	items := make([]LocalOnly, 0, len(names))
	for _, name := range names {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		items = append(items, LocalOnly{Path: path, Kind: KindNoOrigin, Reason: "no origin", Name: name})
	}
	return items
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestCleanLocalPerRepoAnswers(t *testing.T) {
	root := t.TempDir()
	items := makeDirs(t, root, "a", "b", "c")
	var out bytes.Buffer
	s := newSession(t, bareProvider(t), []string{root}, &out)
	c := newScriptedConfirmer(strings.NewReader("yes\nskip\nyes\n"), &out, false)

	deleted, failed := s.cleanLocal(items, c)
	if deleted != 2 || failed != 0 {
		t.Fatalf("deleted = %d, failed = %d", deleted, failed)
	}
	if exists(items[0].Path) || !exists(items[1].Path) || exists(items[2].Path) {
		t.Fatalf("wrong directories removed")
	}
}

func TestCleanLocalSkipToAllStops(t *testing.T) {
	root := t.TempDir()
	items := makeDirs(t, root, "a", "b", "c")
	var out bytes.Buffer
	s := newSession(t, bareProvider(t), []string{root}, &out)
	c := newScriptedConfirmer(strings.NewReader("yes\nskip-to-all\nyes\n"), &out, false)

	deleted, _ := s.cleanLocal(items, c)
	if deleted != 1 {
		t.Fatalf("deleted = %d", deleted)
	}
	if exists(items[0].Path) || !exists(items[1].Path) || !exists(items[2].Path) {
		t.Fatalf("skip-to-all did not stop the command")
	}
}

func TestCleanLocalYesToAllDeletesRest(t *testing.T) {
	root := t.TempDir()
	items := makeDirs(t, root, "a", "b", "c")
	var out bytes.Buffer
	s := newSession(t, bareProvider(t), []string{root}, &out)
	c := newScriptedConfirmer(strings.NewReader("yes-to-all\n"), &out, false)

	deleted, failed := s.cleanLocal(items, c)
	if deleted != 3 || failed != 0 {
		t.Fatalf("deleted = %d, failed = %d", deleted, failed)
	}
	for _, item := range items {
		if exists(item.Path) {
			t.Fatalf("%s survived", item.Path)
		}
	}
}

func TestCleanLocalWarnsAboutDirtyTree(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dirty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "--initial-branch=main", "--quiet")
	commit(t, dir, "a.txt")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	s := newSession(t, bareProvider(t), []string{root}, &out)
	c := newScriptedConfirmer(strings.NewReader("skip\n"), &out, false)
	s.cleanLocal([]LocalOnly{{Path: dir, Kind: KindNoOrigin, Reason: "no origin"}}, c)
	if !strings.Contains(out.String(), "незакоммиченные изменения") {
		t.Fatalf("no dirty warning: %q", out.String())
	}
}

func TestCleanRemoteAnswers(t *testing.T) {
	prov := bareProvider(t)
	items := []RemoteOnly{
		{FullName: "tester/a", repo: provider.Repo{Owner: "tester", Name: "a"}},
		{FullName: "tester/b", repo: provider.Repo{Owner: "tester", Name: "b"}},
		{FullName: "tester/c", repo: provider.Repo{Owner: "tester", Name: "c"}},
	}
	var out bytes.Buffer
	s := newSession(t, prov, []string{t.TempDir()}, &out)
	c := newScriptedConfirmer(strings.NewReader("skip\nyes\nyes\n"), &out, false)

	deleted, failed := s.cleanRemote(items, c)
	if deleted != 2 || failed != 0 {
		t.Fatalf("deleted = %d, failed = %d", deleted, failed)
	}
	if strings.Join(prov.deleted, ",") != "tester/b,tester/c" {
		t.Fatalf("deleted = %v", prov.deleted)
	}
}

func TestCleanRemoteCountsErrors(t *testing.T) {
	prov := bareProvider(t)
	prov.failOn = map[string]error{"delete:tester/a": errors.New("no delete_repo scope")}
	items := []RemoteOnly{{FullName: "tester/a", repo: provider.Repo{Owner: "tester", Name: "a"}}}
	var out bytes.Buffer
	s := newSession(t, prov, []string{t.TempDir()}, &out)
	c := newScriptedConfirmer(strings.NewReader("yes\n"), &out, false)

	deleted, failed := s.cleanRemote(items, c)
	if deleted != 0 || failed != 1 {
		t.Fatalf("deleted = %d, failed = %d", deleted, failed)
	}
	if !strings.Contains(out.String(), "no delete_repo scope") {
		t.Errorf("error not reported: %q", out.String())
	}
}

func TestCleanRemoteYesToAll(t *testing.T) {
	prov := bareProvider(t)
	items := []RemoteOnly{
		{FullName: "tester/a", repo: provider.Repo{Owner: "tester", Name: "a"}},
		{FullName: "tester/b", repo: provider.Repo{Owner: "tester", Name: "b"}},
	}
	var out bytes.Buffer
	s := newSession(t, prov, []string{t.TempDir()}, &out)
	c := newScriptedConfirmer(strings.NewReader("yes-to-all\n"), &out, false)

	deleted, _ := s.cleanRemote(items, c)
	if deleted != 2 || len(prov.deleted) != 2 {
		t.Fatalf("deleted = %d, %v", deleted, prov.deleted)
	}
}
