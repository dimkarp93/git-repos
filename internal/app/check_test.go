package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/scan"
)

type fakeProvider struct {
	repos     []provider.Repo
	heads     map[string]string
	remote    map[string]provider.CompareStatus
	created   []string
	deleted   []string
	renamed   []string
	failOn    map[string]error
	canonical map[string]provider.Repo
	lookups   []string
	base      string
	bare      string
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Account(context.Context) (string, error) { return "tester", nil }

func (f *fakeProvider) ParseRemote(remoteURL string) (string, string, bool) {
	rest, ok := strings.CutPrefix(remoteURL, "https://fake.test/")
	if !ok {
		return "", "", false
	}
	owner, name, found := strings.Cut(strings.TrimSuffix(rest, ".git"), "/")
	return owner, name, found
}

func (f *fakeProvider) ListRepos(context.Context) ([]provider.Repo, error) { return f.repos, nil }

func (f *fakeProvider) Repo(_ context.Context, owner, name string) (provider.Repo, error) {
	f.lookups = append(f.lookups, owner+"/"+name)
	if repo, ok := f.canonical[strings.ToLower(owner+"/"+name)]; ok {
		return repo, nil
	}
	for _, repo := range f.repos {
		if strings.EqualFold(repo.FullName(), owner+"/"+name) {
			return repo, nil
		}
	}
	return provider.Repo{}, provider.ErrNotFound
}

func (f *fakeProvider) BranchHead(_ context.Context, owner, name, branch string) (string, error) {
	sha, ok := f.heads[owner+"/"+name+"@"+branch]
	if !ok {
		return "", provider.ErrNotFound
	}
	return sha, nil
}

func (f *fakeProvider) Compare(_ context.Context, owner, name, base, head string) (provider.CompareStatus, error) {
	status, ok := f.remote[owner+"/"+name]
	if !ok {
		return provider.CompareUnknown, provider.ErrNotFound
	}
	return status, nil
}

func (f *fakeProvider) CreateRepo(_ context.Context, name string, private bool) (provider.Repo, error) {
	if err := f.failOn["create:"+name]; err != nil {
		return provider.Repo{}, err
	}
	if !private {
		return provider.Repo{}, errors.New("repositories must be private")
	}
	if f.bare != "" {
		cmd := exec.Command("git", "init", "--bare", "--quiet", name+".git")
		cmd.Dir = f.bare
		if out, err := cmd.CombinedOutput(); err != nil {
			return provider.Repo{}, fmt.Errorf("init bare: %v: %s", err, out)
		}
	}
	f.created = append(f.created, name)
	repo := provider.Repo{
		Owner:         "tester",
		Name:          name,
		DefaultBranch: "main",
		Private:       true,
		WebURL:        "https://fake.test/tester/" + name,
		CloneURL:      f.base + "/" + name + ".git",
		SSHURL:        f.base + "/" + name + ".git",
	}
	f.repos = append(f.repos, repo)
	return repo, nil
}

func (f *fakeProvider) DeleteRepo(_ context.Context, owner, name string) error {
	if err := f.failOn["delete:"+owner+"/"+name]; err != nil {
		return err
	}
	f.deleted = append(f.deleted, owner+"/"+name)
	return nil
}

func (f *fakeProvider) RenameRepo(_ context.Context, owner, name, newName string) (provider.Repo, error) {
	if err := f.failOn["rename:"+owner+"/"+name]; err != nil {
		return provider.Repo{}, err
	}
	for i, repo := range f.repos {
		if repo.Owner == owner && repo.Name == name {
			repo.Name = newName
			repo.WebURL = "https://fake.test/" + owner + "/" + newName
			f.repos[i] = repo
			f.renamed = append(f.renamed, owner+"/"+name+"->"+newName)
			return repo, nil
		}
	}
	return provider.Repo{}, provider.ErrNotFound
}

func (f *fakeProvider) RemoteURL(repo provider.Repo, protocol string) string {
	if protocol == provider.ProtocolHTTPS && repo.CloneURL != "" {
		return repo.CloneURL
	}
	if repo.SSHURL != "" {
		return repo.SSHURL
	}
	return "https://fake.test/" + repo.Owner + "/" + repo.Name + ".git"
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", name)
	return git(t, dir, "rev-parse", "HEAD")
}

func newRepo(t *testing.T, parent, full string) string {
	t.Helper()
	dir := filepath.Join(parent, filepath.Base(full))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "--initial-branch=main", "--quiet")
	git(t, dir, "remote", "add", "origin", "https://fake.test/"+full+".git")
	return dir
}

func inspectOne(t *testing.T, dir string, prov *fakeProvider, full string, opts options) Result {
	t.Helper()
	owner, name, _ := strings.Cut(full, "/")
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pair := matched{
		local:  scan.Repo{Path: dir},
		remote: provider.Repo{Owner: owner, Name: name, DefaultBranch: "main"},
	}
	return inspect(context.Background(), prov, store, pair, opts)
}

func TestInspectStatuses(t *testing.T) {
	root := t.TempDir()
	dir := newRepo(t, root, "o/n")
	first := commit(t, dir, "a.txt")
	second := commit(t, dir, "b.txt")
	git(t, dir, "checkout", "--quiet", "-b", "side", first)
	side := commit(t, dir, "c.txt")
	git(t, dir, "checkout", "--quiet", "main")

	cases := []struct {
		name   string
		head   string
		want   Status
		detail string
	}{
		{"synced", second, StatusSynced, ""},
		{"remote behind", first, StatusPush, ""},
		{"diverged", side, StatusConflict, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov := &fakeProvider{heads: map[string]string{"o/n@main": tc.head}}
			res := inspectOne(t, dir, prov, "o/n", options{})
			if res.Status != tc.want {
				t.Fatalf("status = %q (%s), want %q", res.Status, res.Detail, tc.want)
			}
		})
	}

	git(t, dir, "reset", "--hard", "--quiet", first)
	prov := &fakeProvider{heads: map[string]string{"o/n@main": second}}
	if res := inspectOne(t, dir, prov, "o/n", options{}); res.Status != StatusPull {
		t.Fatalf("status = %q, want %q", res.Status, StatusPull)
	}
}

func TestInspectUnknownCommitFallsBackToProvider(t *testing.T) {
	root := t.TempDir()
	dir := newRepo(t, root, "o/n")
	commit(t, dir, "a.txt")

	prov := &fakeProvider{
		heads:  map[string]string{"o/n@main": "0000000000000000000000000000000000000000"},
		remote: map[string]provider.CompareStatus{"o/n": provider.CompareBehind},
	}
	res := inspectOne(t, dir, prov, "o/n", options{})
	if res.Status != StatusPull {
		t.Fatalf("status = %q, want %q", res.Status, StatusPull)
	}

	prov.remote = nil
	res = inspectOne(t, dir, prov, "o/n", options{})
	if res.Status != StatusUnknown || !strings.Contains(res.Detail, "--fetch") {
		t.Fatalf("status = %q, detail = %q", res.Status, res.Detail)
	}
}

func TestInspectNoLocalBranch(t *testing.T) {
	root := t.TempDir()
	dir := newRepo(t, root, "o/n")
	commit(t, dir, "a.txt")
	git(t, dir, "branch", "-m", "main", "other")

	prov := &fakeProvider{heads: map[string]string{"o/n@main": "abc"}}
	res := inspectOne(t, dir, prov, "o/n", options{})
	if res.Status != StatusNoBranch {
		t.Fatalf("status = %q, want %q", res.Status, StatusNoBranch)
	}
}

func TestBuildSplitsLocalAndRemoteOnly(t *testing.T) {
	root := t.TempDir()
	shared := newRepo(t, root, "o/shared")
	head := commit(t, shared, "a.txt")

	orphan := newRepo(t, root, "o/orphan")
	commit(t, orphan, "a.txt")

	noRemote := filepath.Join(root, "solo")
	if err := os.MkdirAll(noRemote, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, noRemote, "init", "--initial-branch=main", "--quiet")
	commit(t, noRemote, "a.txt")

	prov := &fakeProvider{
		repos: []provider.Repo{
			{Owner: "o", Name: "shared", DefaultBranch: "main"},
			{Owner: "o", Name: "cloud", DefaultBranch: "main"},
		},
		heads: map[string]string{"o/shared@main": head},
	}
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	report, err := build(context.Background(), prov, store, []string{root}, config.Default(), options{jobs: 2}, &progress{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Repos) != 1 || report.Repos[0].Status != StatusSynced {
		t.Fatalf("repos = %+v", report.Repos)
	}
	if len(report.RemoteOnly) != 1 || report.RemoteOnly[0].FullName != "o/cloud" {
		t.Fatalf("remote only = %+v", report.RemoteOnly)
	}
	if len(report.LocalOnly) != 2 {
		t.Fatalf("local only = %+v", report.LocalOnly)
	}
	reasons := map[string]string{}
	for _, item := range report.LocalOnly {
		reasons[filepath.Base(item.Path)] = item.Reason
	}
	if reasons["solo"] != "no origin" {
		t.Errorf("solo reason = %q", reasons["solo"])
	}
	if !strings.Contains(reasons["orphan"], "not on fake") {
		t.Errorf("orphan reason = %q", reasons["orphan"])
	}
	if !report.diverges() {
		t.Error("diverges = false")
	}
}

func repoWithOrigin(t *testing.T, parent, dir, origin string) string {
	t.Helper()
	path := filepath.Join(parent, dir)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, path, "init", "--initial-branch=main", "--quiet")
	git(t, path, "remote", "add", "origin", origin)
	commit(t, path, "a.txt")
	return path
}

func TestCollectMatchesOriginCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	repoWithOrigin(t, root, "tool", "https://fake.test/tester/fill_food_ai.git")

	prov := &fakeProvider{repos: []provider.Repo{{Owner: "tester", Name: "Fill_Food_AI", DefaultBranch: "main"}}}
	pairs, inv, err := collect(context.Background(), prov, nil, config.Default(), []string{root}, options{}, &progress{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || len(inv.localOnly) != 0 || len(inv.remoteOnly) != 0 {
		t.Fatalf("pairs = %d, localOnly = %+v, remoteOnly = %+v", len(pairs), inv.localOnly, inv.remoteOnly)
	}
	if len(prov.lookups) != 0 {
		t.Errorf("unnecessary API lookups: %v", prov.lookups)
	}
	if len(inv.mismatched) != 1 || inv.mismatched[0].Origin != "tester/fill_food_ai" || inv.mismatched[0].Canonical != "tester/Fill_Food_AI" {
		t.Fatalf("mismatched = %+v", inv.mismatched)
	}
}

func TestCollectFollowsRenamedRepository(t *testing.T) {
	root := t.TempDir()
	path := repoWithOrigin(t, root, "old", "https://fake.test/tester/old-name.git")

	renamed := provider.Repo{Owner: "tester", Name: "new-name", DefaultBranch: "main"}
	prov := &fakeProvider{
		repos:     []provider.Repo{renamed},
		canonical: map[string]provider.Repo{"tester/old-name": renamed},
	}
	pairs, inv, err := collect(context.Background(), prov, nil, config.Default(), []string{root}, options{}, &progress{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].remote.FullName() != "tester/new-name" {
		t.Fatalf("pairs = %+v", pairs)
	}
	if pairs[0].originName != "tester/old-name" {
		t.Errorf("originName = %q", pairs[0].originName)
	}
	if len(inv.remoteOnly) != 0 || len(inv.localOnly) != 0 {
		t.Fatalf("remoteOnly = %+v, localOnly = %+v", inv.remoteOnly, inv.localOnly)
	}
	if len(inv.mismatched) != 1 || inv.mismatched[0].Path != path || inv.mismatched[0].Canonical != "tester/new-name" {
		t.Fatalf("mismatched = %+v", inv.mismatched)
	}
	if len(prov.lookups) != 1 || prov.lookups[0] != "tester/old-name" {
		t.Fatalf("lookups = %v", prov.lookups)
	}
}

func TestCollectKeepsMissingRepositoryLocalOnly(t *testing.T) {
	root := t.TempDir()
	repoWithOrigin(t, root, "gone", "https://fake.test/tester/gone.git")

	prov := &fakeProvider{}
	pairs, inv, err := collect(context.Background(), prov, nil, config.Default(), []string{root}, options{}, &progress{})
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 0 || len(inv.localOnly) != 1 {
		t.Fatalf("pairs = %d, localOnly = %+v", len(pairs), inv.localOnly)
	}
	if inv.localOnly[0].Kind != KindMissingRemote {
		t.Errorf("kind = %q", inv.localOnly[0].Kind)
	}
	if len(inv.mismatched) != 0 {
		t.Errorf("mismatched = %+v", inv.mismatched)
	}
}

func TestCollectRemoteOnlyCountsRenamedRepoOnce(t *testing.T) {
	root := t.TempDir()
	repoWithOrigin(t, root, "old", "https://fake.test/tester/old-name.git")

	renamed := provider.Repo{Owner: "tester", Name: "new-name", DefaultBranch: "main"}
	other := provider.Repo{Owner: "tester", Name: "cloud", DefaultBranch: "main"}
	prov := &fakeProvider{
		repos:     []provider.Repo{renamed, other},
		canonical: map[string]provider.Repo{"tester/old-name": renamed},
	}
	_, inv, err := collect(context.Background(), prov, nil, config.Default(), []string{root}, options{}, &progress{})
	if err != nil {
		t.Fatal(err)
	}
	if len(inv.remoteOnly) != 1 || inv.remoteOnly[0].FullName != "tester/cloud" {
		t.Fatalf("remoteOnly = %+v", inv.remoteOnly)
	}
}
