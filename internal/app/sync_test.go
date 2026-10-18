package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/git-repos/internal/scan"
)

func newSession(t *testing.T, prov *fakeProvider, roots []string, out *bytes.Buffer) *session {
	t.Helper()

	store, err := cache.Open(t.TempDir())
	require.NoError(t, err)
	cfg := config.Default()
	cfg.Protocol = provider.ProtocolHTTPS
	return &session{
		ctx:     context.Background(),
		cfg:     cfg,
		roots:   roots,
		prov:    prov,
		store:   store,
		printer: render.NewPrinter(out, false),
		pr:      &progress{},
		spinner: render.NewSpinner(out, false),
	}
}

func bareProvider(t *testing.T) *fakeProvider {
	t.Helper()
	base := t.TempDir()
	prov := &fakeProvider{base: base, bare: base}
	return prov
}

type actionState struct {
	Done, Plan, Failed bool
}

func statesOf(actions []syncAction) []actionState {
	out := make([]actionState, 0, len(actions))
	for _, a := range actions {
		out = append(out, actionState{Done: a.Done, Plan: a.Plan, Failed: a.Err != nil})
	}
	return out
}

func soloRepo(t *testing.T, root, branch string) (string, string) {
	t.Helper()

	solo := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(solo, 0o755))
	git(t, solo, "init", "--initial-branch="+branch, "--quiet")

	return solo, commit(t, solo, "a.txt")
}

func soloInventory(solo string) inventory {
	return inventory{
		account:   "tester",
		localOnly: []LocalOnly{{Path: solo, Kind: KindNoOrigin, Reason: "no origin", Owner: "tester", Name: "solo"}},
	}
}

type pushedSolo struct {
	prov    *fakeProvider
	solo    string
	head    string
	actions []syncAction
}

func pushSolo(t *testing.T) pushedSolo {
	t.Helper()

	root := t.TempDir()
	solo, head := soloRepo(t, root, "main")
	prov := bareProvider(t)
	s := newSession(t, prov, []string{root}, &bytes.Buffer{})

	actions := s.pushMissing(soloInventory(solo), options{})
	require.Len(t, actions, 1)
	return pushedSolo{prov: prov, solo: solo, head: head, actions: actions}
}

func TestPushMissingCreatesPrivateRepoAndPushes(t *testing.T) {
	p := pushSolo(t)

	t.Run("action", func(t *testing.T) {
		assert.Equal(t, []actionState{{Done: true}}, statesOf(p.actions))
	})
	t.Run("created", func(t *testing.T) {
		assert.Equal(t, []string{"solo"}, p.prov.created)
	})
	t.Run("url", func(t *testing.T) {
		assert.Equal(t, "https://fake.test/tester/solo", p.actions[0].URL)
	})
	t.Run("origin", func(t *testing.T) {
		assert.Contains(t, git(t, p.solo, "config", "--get", "remote.origin.url"), "solo.git")
	})
	t.Run("pushed", func(t *testing.T) {
		assert.Equal(t, p.head, git(t, filepath.Join(p.prov.bare, "solo.git"), "rev-parse", "main"))
	})
}

func skippedPushes(t *testing.T) ([]syncAction, *fakeProvider) {
	t.Helper()

	root := t.TempDir()
	prov := bareProvider(t)
	prov.repos = []provider.Repo{{Owner: "tester", Name: "taken"}}
	s := newSession(t, prov, []string{root}, &bytes.Buffer{})
	inv := inventory{
		account: "tester",
		remotes: prov.repos,
		localOnly: []LocalOnly{
			{Path: filepath.Join(root, "a"), Kind: KindMissingRemote, Owner: "someone", Name: "a"},
			{Path: filepath.Join(root, "taken"), Kind: KindNoOrigin, Owner: "tester", Name: "taken"},
			{Path: filepath.Join(root, "gitlab"), Kind: KindForeignRemote, Reason: "foreign remote"},
		},
	}

	actions := s.pushMissing(inv, options{})
	require.Len(t, actions, 2)
	return actions, prov
}

func TestPushMissingSkipsForeignOwnerAndTakenName(t *testing.T) {
	actions, prov := skippedPushes(t)

	t.Run("foreign owner", func(t *testing.T) {
		assert.Contains(t, actions[0].Detail, "another owner")
	})
	t.Run("taken name", func(t *testing.T) {
		assert.Contains(t, actions[1].Detail, "conflict")
	})
	t.Run("nothing created", func(t *testing.T) {
		assert.Empty(t, prov.created)
	})
}

func pushOne(t *testing.T, prov *fakeProvider, name string, opts options) []syncAction {
	t.Helper()

	root := t.TempDir()
	s := newSession(t, prov, []string{root}, &bytes.Buffer{})
	inv := inventory{account: "tester", localOnly: []LocalOnly{{Path: filepath.Join(root, name), Kind: KindNoOrigin, Owner: "tester", Name: name}}}
	return s.pushMissing(inv, opts)
}

func TestPushMissingDryRun(t *testing.T) {
	prov := bareProvider(t)
	actions := pushOne(t, prov, "x", options{dryRun: true})

	t.Run("plan", func(t *testing.T) {
		assert.Equal(t, []actionState{{Plan: true}}, statesOf(actions))
	})
	t.Run("detail", func(t *testing.T) {
		assert.Contains(t, actions[0].Detail, "would create a private tester/x")
	})
	t.Run("nothing created", func(t *testing.T) {
		assert.Empty(t, prov.created)
	})
}

func TestPushMissingReportsCreateError(t *testing.T) {
	prov := bareProvider(t)
	prov.failOn = map[string]error{"create:boom": errors.New("api down")}

	actions := pushOne(t, prov, "boom", options{})

	assert.Equal(t, []actionState{{Failed: true}}, statesOf(actions))
}

func cloudRemote(t *testing.T, prov *fakeProvider) (provider.Repo, string) {
	t.Helper()

	source := filepath.Join(prov.bare, "cloud.git")
	git(t, prov.bare, "init", "--bare", "--initial-branch=main", "--quiet", "cloud.git")
	seed, head := soloRepo(t, t.TempDir(), "main")
	git(t, seed, "remote", "add", "origin", source)
	git(t, seed, "push", "--quiet", "origin", "main")

	return provider.Repo{Owner: "tester", Name: "cloud", DefaultBranch: "main", CloneURL: source, SSHURL: source}, head
}

func TestCloneMissingClonesIntoRoot(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	repo, head := cloudRemote(t, prov)
	s := newSession(t, prov, []string{root}, &bytes.Buffer{})

	actions := s.cloneMissing(inventory{account: "tester", remoteOnly: []RemoteOnly{{FullName: "tester/cloud", repo: repo}}}, root, options{})

	t.Run("action", func(t *testing.T) {
		assert.Equal(t, []actionState{{Done: true}}, statesOf(actions))
	})
	t.Run("cloned", func(t *testing.T) {
		assert.Equal(t, head, git(t, filepath.Join(root, "cloud"), "rev-parse", "main"))
	})
}

func occupiedRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cloud"), 0o755))

	return root
}

func TestCloneMissingSkipsOccupiedDirectory(t *testing.T) {
	root := occupiedRoot(t)
	s := newSession(t, bareProvider(t), []string{root}, &bytes.Buffer{})
	inv := inventory{remoteOnly: []RemoteOnly{{FullName: "tester/cloud", repo: provider.Repo{Owner: "tester", Name: "cloud"}}}}

	actions := s.cloneMissing(inv, root, options{})

	t.Run("skipped", func(t *testing.T) {
		assert.Equal(t, []actionState{{}}, statesOf(actions))
	})
	t.Run("detail", func(t *testing.T) {
		assert.Contains(t, actions[0].Detail, "conflict")
	})
}

func TestCloneMissingDryRun(t *testing.T) {
	root := t.TempDir()
	s := newSession(t, bareProvider(t), []string{root}, &bytes.Buffer{})
	inv := inventory{remoteOnly: []RemoteOnly{{FullName: "tester/cloud", repo: provider.Repo{Owner: "tester", Name: "cloud"}}}}

	actions := s.cloneMissing(inv, root, options{dryRun: true})

	assert.Equal(t, []actionState{{Plan: true}}, statesOf(actions))
}

func phasedSession(t *testing.T) (*session, string, string) {
	t.Helper()

	root := t.TempDir()
	solo, _ := soloRepo(t, root, "main")
	s := newSession(t, bareProvider(t), []string{root}, &bytes.Buffer{})
	s.pr.setPlan(5)
	for _, phase := range []string{"local scan", "github api", "matching"} {
		s.pr.setPhase(phase)
	}
	return s, root, solo
}

func TestSyncPhasesReportProgress(t *testing.T) {
	s, root, solo := phasedSession(t)

	t.Run("create phase", func(t *testing.T) {
		s.pushMissing(soloInventory(solo), options{})
		assert.Equal(t, "phase 4/5 · creating on fake · 100% (1 of 1 repositories)", s.pr.label())
	})
	t.Run("clone phase", func(t *testing.T) {
		s.cloneMissing(inventory{}, root, options{})
		assert.Regexp(t, `^phase 5/5 · cloning into `, s.pr.label())
	})
}

func emptyRemote(t *testing.T, pushed bool) (*session, []matched, string, string) {
	t.Helper()

	root := t.TempDir()
	prov := bareProvider(t)
	created, err := prov.CreateRepo(context.Background(), "solo", true)
	require.NoError(t, err)
	created.DefaultBranch = "master"

	local, head := soloRepo(t, root, "master")
	git(t, local, "remote", "add", "origin", prov.RemoteURL(created, ""))
	if pushed {
		git(t, local, "push", "--quiet", "origin", "master")
		git(t, local, "fetch", "--quiet", "origin")
	}

	s := newSession(t, prov, []string{root}, &bytes.Buffer{})
	return s, []matched{{local: scan.Repo{Path: local}, remote: created}}, local, head
}

func remoteMaster(t *testing.T, local string) string {
	t.Helper()
	return strings.TrimSpace(git(t, local, "ls-remote", "origin", "refs/heads/master"))
}

func TestFillEmptyPushesIntoEmptyRemote(t *testing.T) {
	s, pairs, local, head := emptyRemote(t, false)

	actions := s.fillEmpty(pairs, options{})

	t.Run("action", func(t *testing.T) {
		assert.Equal(t, []actionState{{Done: true}}, statesOf(actions))
	})
	t.Run("pushed", func(t *testing.T) {
		assert.True(t, strings.HasPrefix(remoteMaster(t, local), head))
	})
}

func TestFillEmptySkipsRepositoriesWithRemoteRefs(t *testing.T) {
	s, pairs, _, _ := emptyRemote(t, true)

	assert.Empty(t, s.fillEmpty(pairs, options{}))
}

func TestFillEmptyDryRun(t *testing.T) {
	s, pairs, local, _ := emptyRemote(t, false)

	actions := s.fillEmpty(pairs, options{dryRun: true})

	t.Run("plan", func(t *testing.T) {
		assert.Equal(t, []actionState{{Plan: true}}, statesOf(actions))
	})
	t.Run("nothing pushed", func(t *testing.T) {
		assert.Empty(t, remoteMaster(t, local))
	})
}

func TestSyncStatus(t *testing.T) {
	cases := map[string]syncAction{
		"error: x": {Err: errors.New("x"), Done: true},
		"done":     {Done: true},
		"plan":     {Plan: true},
		"skipped":  {},
	}
	for want, action := range cases {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, syncStatus(action).Text)
		})
	}
}
