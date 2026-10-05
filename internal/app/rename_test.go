package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/git-repos/internal/scan"
)

func renameInventory(locals []string, remotes []string) inventory {
	inv := inventory{}
	for _, path := range locals {
		inv.locals = append(inv.locals, scan.Repo{Path: path})
	}
	for _, name := range remotes {
		inv.remotes = append(inv.remotes, provider.Repo{Owner: "tester", Name: name})
	}
	for _, local := range inv.locals {
		for _, remote := range inv.remotes {
			if filepath.Base(local.Path) == remote.Name {
				inv.pairs = append(inv.pairs, matched{local: local, remote: remote})
			}
		}
	}
	return inv
}

func TestValidateRepoName(t *testing.T) {
	for _, name := range []string{"tool", "my-tool_2.0", "A"} {
		if err := validateRepoName(name); err != nil {
			t.Errorf("validateRepoName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", ".", "..", "a/b", "a b", "ы"} {
		if err := validateRepoName(name); err == nil {
			t.Errorf("validateRepoName(%q) accepted", name)
		}
	}
}

func TestFindRenamePair(t *testing.T) {
	inv := renameInventory([]string{"/w/a/tool", "/w/b/other"}, []string{"tool", "other"})
	pair, err := findRenamePair(inv, "tool")
	if err != nil || pair.local.Path != "/w/a/tool" || pair.remote.Name != "tool" {
		t.Fatalf("pair = %+v, err = %v", pair, err)
	}

	cases := []struct {
		name    string
		inv     inventory
		old     string
		wantErr string
	}{
		{"no local", renameInventory(nil, []string{"tool"}), "tool", "no local repository"},
		{"two locals", renameInventory([]string{"/a/tool", "/b/tool"}, []string{"tool"}), "tool", "2 local repositories"},
		{"no remote", renameInventory([]string{"/a/tool"}, nil), "tool", "no remote repository"},
		{"two remotes", renameInventory([]string{"/a/tool"}, []string{"tool", "Tool"}), "tool", "2 remote repositories"},
		{"not linked", func() inventory {
			inv := renameInventory([]string{"/a/tool"}, []string{"tool"})
			inv.pairs = nil
			return inv
		}(), "tool", "not linked"},
		{"linked elsewhere", func() inventory {
			inv := renameInventory([]string{"/a/tool"}, []string{"tool"})
			inv.pairs[0].remote = provider.Repo{Owner: "tester", Name: "moved"}
			return inv
		}(), "tool", "not linked"},
	}
	for _, tc := range cases {
		_, err := findRenamePair(tc.inv, tc.old)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestCheckNewName(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	inv := renameInventory(
		[]string{filepath.Join(dir, "tool"), filepath.Join(dir, "other")},
		[]string{"tool", "other", "remote-only"},
	)
	pair := inv.pairs[0]
	cases := []struct {
		name    string
		wantErr string
	}{
		{"fresh", ""},
		{"tool", "local repository"},
		{"OTHER", "local repository"},
		{"remote-only", "remote repository"},
		{"plain", "already exists"},
		{"bad/name", "contains"},
		{"", "empty"},
	}
	for _, tc := range cases {
		err := checkNewName(inv, pair, tc.name)
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("checkNewName(%q) = %v", tc.name, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("checkNewName(%q) = %v, want %q", tc.name, err, tc.wantErr)
		}
	}
}

func TestRenameCandidates(t *testing.T) {
	var locals, remotes []string
	for i := 0; i < 25; i++ {
		name := "repo" + string(rune('a'+i))
		locals = append(locals, filepath.Join("/w", name))
		remotes = append(remotes, name)
	}
	locals = append(locals, "/x/dup", "/y/dup", "/w/local-only")
	remotes = append(remotes, "dup", "remote-only")
	got := renameCandidates(context.Background(), renameInventory(locals, remotes))
	if len(got) != maxRenameCandidates {
		t.Fatalf("candidates = %d, want %d", len(got), maxRenameCandidates)
	}
	for _, c := range got {
		if strings.HasPrefix(c.name, "dup") || c.name == "local-only" || c.name == "remote-only" {
			t.Fatalf("unexpected candidate %q", c.name)
		}
	}
}

func TestRenameCandidatesSortedByActivity(t *testing.T) {
	dir := t.TempDir()
	var locals, remotes []string
	for _, name := range []string{"old", "new"} {
		path := filepath.Join(dir, name)
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		git(t, path, "init", "-q")
		locals = append(locals, path)
		remotes = append(remotes, name)
	}
	past := mustTime(t, "2020-01-01T00:00:00Z")
	for _, file := range []string{"HEAD", "index", "FETCH_HEAD", "logs/HEAD"} {
		os.Chtimes(filepath.Join(dir, "old", ".git", file), past, past)
	}
	os.Chtimes(filepath.Join(dir, "old", ".git"), past, past)
	got := renameCandidates(context.Background(), renameInventory(locals, remotes))
	if len(got) != 2 || got[0].name != "new" || got[1].name != "old" {
		t.Fatalf("candidates = %+v", got)
	}
}

func TestExecuteRename(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "old")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/old.git")

	remote := provider.Repo{Owner: "tester", Name: "old"}
	fake := &fakeProvider{repos: []provider.Repo{remote}, base: "https://fake.test/tester"}
	var out bytes.Buffer
	s := &session{ctx: context.Background(), prov: fake, printer: render.NewPrinter(&out, false)}
	pair := matched{local: scan.Repo{Path: dir}, remote: remote}

	if err := s.executeRename(pair, "fresh"); err != nil {
		t.Fatal(err)
	}
	if len(fake.renamed) != 1 || fake.renamed[0] != "tester/old->fresh" {
		t.Fatalf("renamed = %v", fake.renamed)
	}
	target := filepath.Join(root, "fresh")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("directory was not renamed: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("old directory still exists: %v", err)
	}
	if url := git(t, target, "config", "--get", "remote.origin.url"); !strings.HasSuffix(url, "/fresh.git") {
		t.Fatalf("origin = %q", url)
	}
	if !strings.Contains(out.String(), "Renamed old → fresh") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestExecuteRenameRemoteFailureKeepsLocal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "old")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "-q")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/old.git")

	remote := provider.Repo{Owner: "tester", Name: "old"}
	fake := &fakeProvider{repos: []provider.Repo{remote}, failOn: map[string]error{"rename:tester/old": provider.ErrForbidden}}
	s := &session{ctx: context.Background(), prov: fake, printer: render.NewPrinter(&bytes.Buffer{}, false)}
	err := s.executeRename(matched{local: scan.Repo{Path: dir}, remote: remote}, "fresh")
	if !errors.Is(err, provider.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("local directory must stay: %v", err)
	}
}

func TestProtocolOf(t *testing.T) {
	if protocolOf("https://github.com/o/n.git") != provider.ProtocolHTTPS {
		t.Error("https url")
	}
	if protocolOf("git@github.com:o/n.git") != provider.ProtocolSSH {
		t.Error("ssh url")
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
