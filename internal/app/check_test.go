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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	histories map[string]provider.History
	trees     map[string][]provider.TreeEntry
	treeCalls []string
}

func (f *fakeProvider) History(_ context.Context, owner, name, branch string) (provider.History, error) {
	h, ok := f.histories[owner+"/"+name]
	if !ok {
		return provider.History{}, provider.ErrEmpty
	}
	return h, nil
}

func (f *fakeProvider) Tree(_ context.Context, owner, name, branch string) ([]provider.TreeEntry, error) {
	f.treeCalls = append(f.treeCalls, owner+"/"+name)
	entries, ok := f.trees[owner+"/"+name]
	if !ok {
		return nil, provider.ErrNotFound
	}
	return entries, nil
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
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, name string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644))
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", name)
	return git(t, dir, "rev-parse", "HEAD")
}

func newRepo(t *testing.T, parent, full string) string {
	t.Helper()
	dir := filepath.Join(parent, filepath.Base(full))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	git(t, dir, "init", "--initial-branch=main", "--quiet")
	git(t, dir, "remote", "add", "origin", "https://fake.test/"+full+".git")
	return dir
}

func inspectOne(t *testing.T, dir string, prov *fakeProvider, full string, opts options) Result {
	t.Helper()
	owner, name, _ := strings.Cut(full, "/")
	store, err := cache.Open(t.TempDir())
	require.NoError(t, err)
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
			require.Equal(t, tc.want, res.Status, "status = %q (%s), want %q", res.Status, res.Detail, tc.want)
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
	require.Equal(t, StatusPull, res.Status, "status = %q, want %q", res.Status, StatusPull)

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
	require.Equal(t, StatusNoBranch, res.Status, "status = %q, want %q", res.Status, StatusNoBranch)
}

func TestBuildSplitsLocalAndRemoteOnly(t *testing.T) {
	root := t.TempDir()
	shared := newRepo(t, root, "o/shared")
	head := commit(t, shared, "a.txt")

	orphan := newRepo(t, root, "o/orphan")
	commit(t, orphan, "a.txt")

	noRemote := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(noRemote, 0o755))
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
	require.NoError(t, err)
	report, err := build(context.Background(), prov, store, []string{root}, config.Default(), options{jobs: 2}, &progress{})
	require.NoError(t, err)
	if len(report.Repos) != 1 || report.Repos[0].Status != StatusSynced {
		t.Fatalf("repos = %+v", report.Repos)
	}
	if len(report.RemoteOnly) != 1 || report.RemoteOnly[0].FullName != "o/cloud" {
		t.Fatalf("remote only = %+v", report.RemoteOnly)
	}
	require.Len(t, report.LocalOnly, 2, "local only = %+v", report.LocalOnly)
	reasons := map[string]string{}
	for _, item := range report.LocalOnly {
		reasons[filepath.Base(item.Path)] = item.Reason
	}
	assert.Equal(t, "no origin", reasons["solo"], "solo reason = %q", reasons["solo"])
	assert.Contains(t, reasons["orphan"], "not on fake", "orphan reason = %q", reasons["orphan"])
	assert.True(t, report.diverges(), "diverges = false")
}

func repoWithOrigin(t *testing.T, parent, dir, origin string) string {
	t.Helper()
	path := filepath.Join(parent, dir)
	require.NoError(t, os.MkdirAll(path, 0o755))
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
	require.NoError(t, err)
	if len(pairs) != 1 || len(inv.localOnly) != 0 || len(inv.remoteOnly) != 0 {
		t.Fatalf("pairs = %d, localOnly = %+v, remoteOnly = %+v", len(pairs), inv.localOnly, inv.remoteOnly)
	}
	assert.Len(t, prov.lookups, 0, "unnecessary API lookups: %v", prov.lookups)
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
	require.NoError(t, err)
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
	require.NoError(t, err)
	if len(pairs) != 0 || len(inv.localOnly) != 1 {
		t.Fatalf("pairs = %d, localOnly = %+v", len(pairs), inv.localOnly)
	}
	assert.Equal(t, KindMissingRemote, inv.localOnly[0].Kind, "kind = %q", inv.localOnly[0].Kind)
	assert.Len(t, inv.mismatched, 0, "mismatched = %+v", inv.mismatched)
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
	require.NoError(t, err)
	if len(inv.remoteOnly) != 1 || inv.remoteOnly[0].FullName != "tester/cloud" {
		t.Fatalf("remoteOnly = %+v", inv.remoteOnly)
	}
}

func TestCollectKeepsNonOriginRemoteLocalOnly(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://gitlab.example/x/tool.git")
	git(t, dir, "remote", "add", "github", "https://fake.test/tester/tool.git")

	prov := &fakeProvider{repos: []provider.Repo{{Owner: "tester", Name: "tool", DefaultBranch: "main"}}}
	pairs, inv, err := collect(context.Background(), prov, nil, config.Default(), []string{root}, options{}, &progress{})
	require.NoError(t, err)
	require.Len(t, pairs, 0, "pairs = %+v", pairs)
	if len(inv.localOnly) != 1 || inv.localOnly[0].Kind != KindNonOrigin || inv.localOnly[0].Orphan() {
		t.Fatalf("localOnly = %+v", inv.localOnly)
	}
	require.Len(t, inv.remoteOnly, 0, "remoteOnly = %+v", inv.remoteOnly)
}

func TestCollectPrefersOriginOverOtherRemotes(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	git(t, dir, "remote", "add", "gitea", "https://fake.test/tester/mirror.git")

	prov := &fakeProvider{repos: []provider.Repo{
		{Owner: "tester", Name: "tool", DefaultBranch: "main"},
		{Owner: "tester", Name: "mirror", DefaultBranch: "main"},
	}}
	pairs, inv, err := collect(context.Background(), prov, nil, config.Default(), []string{root}, options{}, &progress{})
	require.NoError(t, err)
	if len(pairs) != 1 || pairs[0].remoteName != "origin" || pairs[0].remote.Name != "tool" {
		t.Fatalf("pairs = %+v", pairs)
	}
	if len(inv.localOnly) != 0 || len(inv.remoteOnly) != 1 || inv.remoteOnly[0].FullName != "tester/mirror" {
		t.Fatalf("localOnly = %+v, remoteOnly = %+v", inv.localOnly, inv.remoteOnly)
	}
}

func TestCollectKeepsOriginKindWhenNoRemoteMatches(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://gitlab.example/x/tool.git")
	git(t, dir, "remote", "add", "github", "https://fake.test/tester/gone.git")

	_, inv, err := collect(context.Background(), &fakeProvider{}, nil, config.Default(), []string{root}, options{}, &progress{})
	require.NoError(t, err)
	if len(inv.localOnly) != 1 || inv.localOnly[0].Kind != KindForeignRemote {
		t.Fatalf("localOnly = %+v", inv.localOnly)
	}
}
