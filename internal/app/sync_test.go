package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/git-repos/internal/scan"
)

func newSession(t *testing.T, prov *fakeProvider, roots []string, out *bytes.Buffer) *session {
	t.Helper()
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
	if err := os.MkdirAll(solo, 0o755); err != nil {
		t.Fatal(err)
	}
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
	if pushed != head {
		t.Errorf("pushed = %q, want %q", pushed, head)
	}
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
	if len(actions) != 2 {
		t.Fatalf("actions = %+v", actions)
	}
	if !strings.Contains(actions[0].Detail, "чужой владелец") {
		t.Errorf("actions[0] = %+v", actions[0])
	}
	if !strings.Contains(actions[1].Detail, "конфликт") {
		t.Errorf("actions[1] = %+v", actions[1])
	}
	if len(prov.created) != 0 {
		t.Fatalf("created = %v", prov.created)
	}
}

func TestPushMissingDryRun(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	inv := inventory{account: "tester", localOnly: []LocalOnly{{Path: filepath.Join(root, "x"), Kind: KindNoOrigin, Owner: "tester", Name: "x"}}}
	actions := s.pushMissing(inv, options{dryRun: true})
	if len(actions) != 1 || actions[0].Done || !strings.Contains(actions[0].Detail, "будет создан приватный tester/x") {
		t.Fatalf("actions = %+v", actions)
	}
	if len(prov.created) != 0 {
		t.Fatalf("created = %v", prov.created)
	}
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
	if err := os.MkdirAll(seed, 0o755); err != nil {
		t.Fatal(err)
	}
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
	if got := git(t, filepath.Join(root, "cloud"), "rev-parse", "main"); got != head {
		t.Errorf("cloned head = %q, want %q", got, head)
	}
}

func TestCloneMissingSkipsOccupiedDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cloud"), 0o755); err != nil {
		t.Fatal(err)
	}
	prov := bareProvider(t)
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	inv := inventory{remoteOnly: []RemoteOnly{{FullName: "tester/cloud", repo: provider.Repo{Owner: "tester", Name: "cloud"}}}}
	actions := s.cloneMissing(inv, root, options{})
	if len(actions) != 1 || actions[0].Done || !strings.Contains(actions[0].Detail, "конфликт") {
		t.Fatalf("actions = %+v", actions)
	}
}

func TestSyncPhasesReportProgress(t *testing.T) {
	root := t.TempDir()
	solo := filepath.Join(root, "solo")
	if err := os.MkdirAll(solo, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, solo, "init", "--initial-branch=main", "--quiet")
	commit(t, solo, "a.txt")

	prov := bareProvider(t)
	var out bytes.Buffer
	s := newSession(t, prov, []string{root}, &out)
	s.pr.setPlan(5)
	s.pr.setPhase("локальный скан")
	s.pr.setPhase("github api")
	s.pr.setPhase("сопоставление")
	inv := inventory{
		account:   "tester",
		localOnly: []LocalOnly{{Path: solo, Kind: KindNoOrigin, Reason: "no origin", Owner: "tester", Name: "solo"}},
	}

	s.pushMissing(inv, options{})
	if got := s.pr.label(); got != "фаза 4/5 · создание на fake · 100% (1 из 1 репозиториев)" {
		t.Fatalf("label = %q", got)
	}

	s.cloneMissing(inventory{}, root, options{})
	if got := s.pr.label(); !strings.HasPrefix(got, "фаза 5/5 · клонирование в ") {
		t.Fatalf("label = %q", got)
	}
}

func TestFillEmptyPushesIntoEmptyRemote(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	created, err := prov.CreateRepo(context.Background(), "solo", true)
	if err != nil {
		t.Fatal(err)
	}
	created.DefaultBranch = "master"

	local := filepath.Join(root, "solo")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
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
	if !strings.HasPrefix(pushed, head) {
		t.Fatalf("ls-remote = %q, want %s", pushed, head)
	}
}

func TestFillEmptySkipsRepositoriesWithRemoteRefs(t *testing.T) {
	root := t.TempDir()
	prov := bareProvider(t)
	created, err := prov.CreateRepo(context.Background(), "solo", true)
	if err != nil {
		t.Fatal(err)
	}
	created.DefaultBranch = "master"

	local := filepath.Join(root, "solo")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
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
	if err != nil {
		t.Fatal(err)
	}
	created.DefaultBranch = "master"

	local := filepath.Join(root, "solo")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
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
