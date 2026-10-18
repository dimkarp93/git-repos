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

func TestPushMissingCreatesPrivateRepoAndPushes(t *testing.T) {
	root := t.TempDir()
	solo := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(solo, 0o755))
	git(t, solo, "init", "--initial-branch=main", "--quiet")
	head := commit(t, solo, "a.txt")

	prov := bareProvider(t)
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	inv := inventory{
		account:   "tester",
		localOnly: []LocalOnly{{Path: solo, Kind: KindNoOrigin, Reason: "no origin", Owner: "tester", Name: "solo"}},
	}

	actions := s.pushMissing(inv, options{})
	if len(actions) != 1 || !actions[0].Done || actions[0].Err != nil {
		t.Fatalf("actions = %+v", actions)
	}
	if len(prov.created) != 1 || prov.created[0] != "solo" {
		t.Fatalf("created = %v", prov.created)
	}
	if actions[0].URL != "https://fake.test/tester/solo" {
		t.Errorf("url = %q", actions[0].URL)
	}
	if url := git(t, solo, "config", "--get", "remote.origin.url"); !strings.Contains(url, "solo.git") {
		t.Errorf("origin = %q", url)
	}
	pushed := git(t, filepath.Join(prov.bare, "solo.git"), "rev-parse", "main")
	assert.Equal(t, head, pushed, "pushed = %q, want %q", pushed, head)
}

func TestPushMissingSkipsForeignOwnerAndTakenName(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	prov.repos = []provider.Repo{{Owner: "tester", Name: "taken"}}
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
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
	require.Len(t, actions, 2, "actions = %+v", actions)
	assert.Contains(t, actions[0].Detail, "another owner", "actions[0] = %+v", actions[0])
	assert.Contains(t, actions[1].Detail, "conflict", "actions[1] = %+v", actions[1])
	require.Len(t, prov.created, 0, "created = %v", prov.created)
}

func TestPushMissingDryRun(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	inv := inventory{account: "tester", localOnly: []LocalOnly{{Path: filepath.Join(root, "x"), Kind: KindNoOrigin, Owner: "tester", Name: "x"}}}
	actions := s.pushMissing(inv, options{dryRun: true})
	if len(actions) != 1 || actions[0].Done || !strings.Contains(actions[0].Detail, "would create a private tester/x") {
		t.Fatalf("actions = %+v", actions)
	}
	require.Len(t, prov.created, 0, "created = %v", prov.created)
}

func TestPushMissingReportsCreateError(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	prov.failOn = map[string]error{"create:boom": errors.New("api down")}
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	inv := inventory{account: "tester", localOnly: []LocalOnly{{Path: filepath.Join(root, "boom"), Kind: KindNoOrigin, Owner: "tester", Name: "boom"}}}
	actions := s.pushMissing(inv, options{})
	if len(actions) != 1 || actions[0].Err == nil || actions[0].Done {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestCloneMissingClonesIntoRoot(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	source := filepath.Join(prov.bare, "cloud.git")
	git(t, prov.bare, "init", "--bare", "--initial-branch=main", "--quiet", "cloud.git")
	seed := filepath.Join(t.TempDir(), "seed")
	require.NoError(t, os.MkdirAll(seed, 0o755))
	git(t, seed, "init", "--initial-branch=main", "--quiet")
	head := commit(t, seed, "a.txt")
	git(t, seed, "remote", "add", "origin", source)
	git(t, seed, "push", "--quiet", "origin", "main")

	repo := provider.Repo{Owner: "tester", Name: "cloud", DefaultBranch: "main", CloneURL: source, SSHURL: source}
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	inv := inventory{account: "tester", remoteOnly: []RemoteOnly{{FullName: "tester/cloud", repo: repo}}}

	actions := s.cloneMissing(inv, root, options{})
	if len(actions) != 1 || !actions[0].Done || actions[0].Err != nil {
		t.Fatalf("actions = %+v", actions)
	}
	assert.Equal(t, head, git(t, filepath.Join(root, "cloud"), "rev-parse", "main"))
}

func TestCloneMissingSkipsOccupiedDirectory(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "cloud"), 0o755))
	prov := bareProvider(t)
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	inv := inventory{remoteOnly: []RemoteOnly{{FullName: "tester/cloud", repo: provider.Repo{Owner: "tester", Name: "cloud"}}}}
	actions := s.cloneMissing(inv, root, options{})
	if len(actions) != 1 || actions[0].Done || !strings.Contains(actions[0].Detail, "conflict") {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestSyncPhasesReportProgress(t *testing.T) {
	root := t.TempDir()
	solo := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(solo, 0o755))
	git(t, solo, "init", "--initial-branch=main", "--quiet")
	commit(t, solo, "a.txt")

	prov := bareProvider(t)
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	s.pr.setPlan(5)
	s.pr.setPhase("local scan")
	s.pr.setPhase("github api")
	s.pr.setPhase("matching")
	inv := inventory{
		account:   "tester",
		localOnly: []LocalOnly{{Path: solo, Kind: KindNoOrigin, Reason: "no origin", Owner: "tester", Name: "solo"}},
	}

	s.pushMissing(inv, options{})
	require.Equal(t, "phase 4/5 · creating on fake · 100% (1 of 1 repositories)", s.pr.label())

	s.cloneMissing(inventory{}, root, options{})
	if got := s.pr.label(); !strings.HasPrefix(got, "phase 5/5 · cloning into ") {
		t.Fatalf("label = %q", got)
	}
}

func TestFillEmptyPushesIntoEmptyRemote(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	created, err := prov.CreateRepo(context.Background(), "solo", true)
	require.NoError(t, err)
	created.DefaultBranch = "master"

	local := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(local, 0o755))
	git(t, local, "init", "--initial-branch=master", "--quiet")
	head := commit(t, local, "a.txt")
	git(t, local, "remote", "add", "origin", prov.RemoteURL(created, ""))

	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	pairs := []matched{{local: scan.Repo{Path: local}, remote: created}}

	actions := s.fillEmpty(pairs, options{})
	if len(actions) != 1 || !actions[0].Done || actions[0].Err != nil {
		t.Fatalf("actions = %+v", actions)
	}
	pushed := strings.TrimSpace(git(t, local, "ls-remote", "origin", "refs/heads/master"))
	require.True(t, strings.HasPrefix(pushed, head), "ls-remote = %q, want %s", pushed, head)
}

func TestFillEmptySkipsRepositoriesWithRemoteRefs(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	created, err := prov.CreateRepo(context.Background(), "solo", true)
	require.NoError(t, err)
	created.DefaultBranch = "master"

	local := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(local, 0o755))
	git(t, local, "init", "--initial-branch=master", "--quiet")
	commit(t, local, "a.txt")
	git(t, local, "remote", "add", "origin", prov.RemoteURL(created, ""))
	git(t, local, "push", "--quiet", "origin", "master")
	git(t, local, "fetch", "--quiet", "origin")

	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	pairs := []matched{{local: scan.Repo{Path: local}, remote: created}}

	if actions := s.fillEmpty(pairs, options{}); len(actions) != 0 {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestFillEmptyDryRun(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	created, err := prov.CreateRepo(context.Background(), "solo", true)
	require.NoError(t, err)
	created.DefaultBranch = "master"

	local := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(local, 0o755))
	git(t, local, "init", "--initial-branch=master", "--quiet")
	commit(t, local, "a.txt")
	git(t, local, "remote", "add", "origin", prov.RemoteURL(created, ""))

	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	pairs := []matched{{local: scan.Repo{Path: local}, remote: created}}

	actions := s.fillEmpty(pairs, options{dryRun: true})
	if len(actions) != 1 || !actions[0].Plan || actions[0].Done {
		t.Fatalf("actions = %+v", actions)
	}
	if out := strings.TrimSpace(git(t, local, "ls-remote", "origin", "refs/heads/master")); out != "" {
		t.Fatalf("dry-run запушил: %q", out)
	}
}
