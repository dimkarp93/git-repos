package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		t.Run("valid "+name, func(t *testing.T) {
			assert.NoError(t, validateRepoName(name))
		})
	}
	for _, name := range []string{"", ".", "..", "a/b", "a b", "ы"} {
		t.Run("invalid "+name, func(t *testing.T) {
			assert.Error(t, validateRepoName(name))
		})
	}
}

func TestFindRenamePair(t *testing.T) {
	inv := renameInventory([]string{"/w/a/tool", "/w/b/other"}, []string{"tool", "other"})

	pair, err := findRenamePair(inv, "tool")

	require.NoError(t, err)
	assert.Equal(t, []string{"/w/a/tool", "tool"}, []string{pair.local.Path, pair.remote.Name})
}

func unlinkedInventory() inventory {
	inv := renameInventory([]string{"/a/tool"}, []string{"tool"})
	inv.pairs = nil
	return inv
}

func linkedElsewhereInventory() inventory {
	inv := renameInventory([]string{"/a/tool"}, []string{"tool"})
	inv.pairs[0].remote = provider.Repo{Owner: "tester", Name: "moved"}
	return inv
}

func TestFindRenamePairErrors(t *testing.T) {
	cases := []struct {
		name    string
		inv     inventory
		wantErr string
	}{
		{"no local", renameInventory(nil, []string{"tool"}), "no local repository"},
		{"two locals", renameInventory([]string{"/a/tool", "/b/tool"}, []string{"tool"}), "2 local repositories"},
		{"no remote", renameInventory([]string{"/a/tool"}, nil), "no remote repository"},
		{"two remotes", renameInventory([]string{"/a/tool"}, []string{"tool", "Tool"}), "2 remote repositories"},
		{"not linked", unlinkedInventory(), "not linked"},
		{"linked elsewhere", linkedElsewhereInventory(), "not linked"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := findRenamePair(tc.inv, "tool")
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func newNameInventory(t *testing.T) (inventory, matched) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "plain"), 0o755))
	inv := renameInventory(
		[]string{filepath.Join(dir, "tool"), filepath.Join(dir, "other")},
		[]string{"tool", "other", "remote-only"},
	)
	return inv, inv.pairs[0]
}

func TestCheckNewName(t *testing.T) {
	inv, pair := newNameInventory(t)
	cases := map[string]string{
		"tool":        "local repository",
		"OTHER":       "local repository",
		"remote-only": "remote repository",
		"plain":       "already exists",
		"bad/name":    "contains",
		"":            "empty",
	}
	for name, wantErr := range cases {
		t.Run(name, func(t *testing.T) {
			assert.ErrorContains(t, checkNewName(inv, pair, name), wantErr)
		})
	}
	t.Run("fresh", func(t *testing.T) {
		assert.NoError(t, checkNewName(inv, pair, "fresh"))
	})
}

func candidateNames(candidates []renameCandidate) []string {
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.name)
	}
	return names
}

func manyCandidates() []renameCandidate {
	var locals, remotes []string
	for i := range 25 {
		name := "repo" + string(rune('a'+i))
		locals = append(locals, filepath.Join("/w", name))
		remotes = append(remotes, name)
	}
	locals = append(locals, "/x/dup", "/y/dup", "/w/local-only")
	remotes = append(remotes, "dup", "remote-only")
	return renameCandidates(context.Background(), renameInventory(locals, remotes))
}

func TestRenameCandidates(t *testing.T) {
	got := candidateNames(manyCandidates())

	t.Run("limited", func(t *testing.T) {
		assert.Len(t, got, maxRenameCandidates)
	})
	for _, unwanted := range []string{"dup", "local-only", "remote-only"} {
		t.Run("skips "+unwanted, func(t *testing.T) {
			assert.NotContains(t, got, unwanted)
		})
	}
}

func agedRepos(t *testing.T) ([]string, []string) {
	t.Helper()

	dir := t.TempDir()
	var locals, remotes []string
	for _, name := range []string{"old", "new"} {
		path := filepath.Join(dir, name)
		require.NoError(t, os.Mkdir(path, 0o755))
		git(t, path, "init", "-q")
		locals = append(locals, path)
		remotes = append(remotes, name)
	}

	past := mustTime(t, "2020-01-01T00:00:00Z")
	for _, file := range []string{"HEAD", "index", "FETCH_HEAD", "logs/HEAD", ""} {
		os.Chtimes(filepath.Join(dir, "old", ".git", file), past, past)
	}
	return locals, remotes
}

func TestRenameCandidatesSortedByActivity(t *testing.T) {
	locals, remotes := agedRepos(t)

	got := renameCandidates(context.Background(), renameInventory(locals, remotes))

	assert.Equal(t, []string{"new", "old"}, candidateNames(got))
}

func renameFixture(t *testing.T, failOn map[string]error) (*session, *fakeProvider, matched, *bytes.Buffer) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "old")
	require.NoError(t, os.Mkdir(dir, 0o755))
	git(t, dir, "init", "-q")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/old.git")

	remote := provider.Repo{Owner: "tester", Name: "old"}
	fake := &fakeProvider{repos: []provider.Repo{remote}, base: "https://fake.test/tester", failOn: failOn}
	var out bytes.Buffer
	s := &session{ctx: context.Background(), prov: fake, printer: render.NewPrinter(&out, false)}
	return s, fake, matched{local: scan.Repo{Path: dir}, remote: remote}, &out
}

func renamedFresh(t *testing.T) (*fakeProvider, matched, *bytes.Buffer) {
	t.Helper()

	s, fake, pair, out := renameFixture(t, nil)
	require.NoError(t, s.executeRename(pair, "fresh"))

	return fake, pair, out
}

func TestExecuteRename(t *testing.T) {
	fake, pair, out := renamedFresh(t)
	target := filepath.Join(filepath.Dir(pair.local.Path), "fresh")

	t.Run("remote renamed", func(t *testing.T) {
		assert.Equal(t, []string{"tester/old->fresh"}, fake.renamed)
	})
	t.Run("directory renamed", func(t *testing.T) {
		assert.DirExists(t, target)
	})
	t.Run("old directory gone", func(t *testing.T) {
		assert.NoDirExists(t, pair.local.Path)
	})
	t.Run("origin updated", func(t *testing.T) {
		assert.True(t, strings.HasSuffix(git(t, target, "config", "--get", "remote.origin.url"), "/fresh.git"))
	})
	t.Run("reported", func(t *testing.T) {
		assert.Contains(t, out.String(), "Renamed old → fresh")
	})
}

func TestExecuteRenameRemoteFailureKeepsLocal(t *testing.T) {
	s, _, pair, _ := renameFixture(t, map[string]error{"rename:tester/old": provider.ErrForbidden})

	err := s.executeRename(pair, "fresh")

	t.Run("error", func(t *testing.T) {
		assert.ErrorIs(t, err, provider.ErrForbidden)
	})
	t.Run("local kept", func(t *testing.T) {
		assert.DirExists(t, pair.local.Path)
	})
}

func TestProtocolOf(t *testing.T) {
	cases := map[string]string{
		"https://github.com/o/n.git": provider.ProtocolHTTPS,
		"git@github.com:o/n.git":     provider.ProtocolSSH,
	}
	for url, want := range cases {
		t.Run(url, func(t *testing.T) {
			assert.Equal(t, want, protocolOf(url))
		})
	}
}

func TestRemoteProtocol(t *testing.T) {
	t.Run("from url", func(t *testing.T) {
		assert.Equal(t, provider.ProtocolHTTPS, remoteProtocol("https://x/o/n.git", provider.ProtocolSSH))
	})
	t.Run("fallback", func(t *testing.T) {
		assert.Equal(t, provider.ProtocolSSH, remoteProtocol("", provider.ProtocolSSH))
	})
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	require.NoError(t, err)
	return parsed
}
