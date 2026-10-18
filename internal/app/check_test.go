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

type inspectRepo struct {
	dir                 string
	first, second, side string
}

func branchyRepo(t *testing.T) inspectRepo {
	t.Helper()

	dir := newRepo(t, t.TempDir(), "o/n")
	r := inspectRepo{dir: dir, first: commit(t, dir, "a.txt")}
	r.second = commit(t, dir, "b.txt")
	git(t, dir, "checkout", "--quiet", "-b", "side", r.first)
	r.side = commit(t, dir, "c.txt")
	git(t, dir, "checkout", "--quiet", "main")
	return r
}

func headAt(sha string) *fakeProvider {
	return &fakeProvider{heads: map[string]string{"o/n@main": sha}}
}

func TestInspectStatuses(t *testing.T) {
	r := branchyRepo(t)
	cases := []struct {
		name string
		head string
		want Status
	}{
		{"synced", r.second, StatusSynced},
		{"remote behind", r.first, StatusPush},
		{"diverged", r.side, StatusConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, inspectOne(t, r.dir, headAt(tc.head), "o/n", options{}).Status)
		})
	}
}

func TestInspectLocalBehind(t *testing.T) {
	r := branchyRepo(t)
	git(t, r.dir, "reset", "--hard", "--quiet", r.first)

	res := inspectOne(t, r.dir, headAt(r.second), "o/n", options{})

	assert.Equal(t, StatusPull, res.Status)
}

func unknownCommitRepo(t *testing.T) string {
	t.Helper()

	dir := newRepo(t, t.TempDir(), "o/n")
	commit(t, dir, "a.txt")
	return dir
}

func TestInspectUnknownCommitFallsBackToProvider(t *testing.T) {
	prov := headAt("0000000000000000000000000000000000000000")
	prov.remote = map[string]provider.CompareStatus{"o/n": provider.CompareBehind}

	res := inspectOne(t, unknownCommitRepo(t), prov, "o/n", options{})

	assert.Equal(t, StatusPull, res.Status)
}

func TestInspectUnknownCommitWithoutProviderAnswer(t *testing.T) {
	res := inspectOne(t, unknownCommitRepo(t), headAt("0000000000000000000000000000000000000000"), "o/n", options{})

	assert.Equal(t, []any{StatusUnknown, true}, []any{res.Status, strings.Contains(res.Detail, "--fetch")})
}

func TestInspectNoLocalBranch(t *testing.T) {
	dir := unknownCommitRepo(t)
	git(t, dir, "branch", "-m", "main", "other")

	res := inspectOne(t, dir, headAt("abc"), "o/n", options{})

	assert.Equal(t, StatusNoBranch, res.Status)
}

func TestCompareStatus(t *testing.T) {
	cases := map[provider.CompareStatus]Status{
		provider.CompareIdentical: StatusSynced,
		provider.CompareAhead:     StatusPush,
		provider.CompareBehind:    StatusPull,
		provider.CompareDiverged:  StatusConflict,
		"weird":                   StatusUnknown,
	}
	for in, want := range cases {
		t.Run(string(in), func(t *testing.T) {
			got, _ := compareStatus(in)
			assert.Equal(t, want, got)
		})
	}
}

func builtReport(t *testing.T) Report {
	t.Helper()

	root := t.TempDir()
	shared := newRepo(t, root, "o/shared")
	head := commit(t, shared, "a.txt")
	commit(t, newRepo(t, root, "o/orphan"), "a.txt")
	noRemote := filepath.Join(root, "solo")
	require.NoError(t, os.MkdirAll(noRemote, 0o755))
	git(t, noRemote, "init", "--initial-branch=main", "--quiet")
	commit(t, noRemote, "a.txt")

	prov := &fakeProvider{
		repos: []provider.Repo{{Owner: "o", Name: "shared", DefaultBranch: "main"}, {Owner: "o", Name: "cloud", DefaultBranch: "main"}},
		heads: map[string]string{"o/shared@main": head},
	}
	store, err := cache.Open(t.TempDir())
	require.NoError(t, err)
	report, err := build(context.Background(), prov, store, []string{root}, config.Default(), options{jobs: 2}, &progress{})
	require.NoError(t, err)
	return report
}

func localReasons(items []LocalOnly) map[string]string {
	reasons := map[string]string{}
	for _, item := range items {
		reasons[filepath.Base(item.Path)] = item.Reason
	}
	return reasons
}

func TestBuildSplitsLocalAndRemoteOnly(t *testing.T) {
	report := builtReport(t)

	t.Run("matched repos", func(t *testing.T) {
		require.Len(t, report.Repos, 1)
		assert.Equal(t, StatusSynced, report.Repos[0].Status)
	})
	t.Run("remote only", func(t *testing.T) {
		assert.Equal(t, []string{"o/cloud"}, remoteNames(report.RemoteOnly))
	})
	t.Run("local only without origin", func(t *testing.T) {
		assert.Equal(t, "no origin", localReasons(report.LocalOnly)["solo"])
	})
	t.Run("local only with missing remote", func(t *testing.T) {
		assert.Contains(t, localReasons(report.LocalOnly)["orphan"], "not on fake")
	})
	t.Run("diverges", func(t *testing.T) {
		assert.True(t, report.diverges())
	})
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

func collected(t *testing.T, prov *fakeProvider, root string) ([]matched, inventory) {
	t.Helper()

	pairs, inv, err := collect(context.Background(), prov, nil, config.Default(), []string{root}, options{}, &progress{})
	require.NoError(t, err)

	return pairs, inv
}

func remoteNames(items []RemoteOnly) []string {
	var names []string
	for _, item := range items {
		names = append(names, item.FullName)
	}
	return names
}

func localKinds(items []LocalOnly) []string {
	var kinds []string
	for _, item := range items {
		kinds = append(kinds, item.Kind)
	}
	return kinds
}

func pairNames(pairs []matched) []string {
	var names []string
	for _, pair := range pairs {
		names = append(names, pair.remoteName+" "+pair.remote.FullName())
	}
	return names
}

func TestCollectMatchesOriginCaseInsensitively(t *testing.T) {
	root := t.TempDir()
	repoWithOrigin(t, root, "tool", "https://fake.test/tester/fill_food_ai.git")
	prov := &fakeProvider{repos: []provider.Repo{{Owner: "tester", Name: "Fill_Food_AI", DefaultBranch: "main"}}}

	pairs, inv := collected(t, prov, root)

	t.Run("paired", func(t *testing.T) {
		assert.Equal(t, []string{"origin tester/Fill_Food_AI"}, pairNames(pairs))
	})
	t.Run("nothing left over", func(t *testing.T) {
		assert.Equal(t, 0, len(inv.localOnly)+len(inv.remoteOnly))
	})
	t.Run("no api lookups", func(t *testing.T) {
		assert.Empty(t, prov.lookups)
	})
	t.Run("mismatch recorded", func(t *testing.T) {
		require.Len(t, inv.mismatched, 1)
		assert.Equal(t, []string{"tester/fill_food_ai", "tester/Fill_Food_AI"}, []string{inv.mismatched[0].Origin, inv.mismatched[0].Canonical})
	})
}

func renamedProvider(extra ...provider.Repo) *fakeProvider {
	renamed := provider.Repo{Owner: "tester", Name: "new-name", DefaultBranch: "main"}
	return &fakeProvider{
		repos:     append([]provider.Repo{renamed}, extra...),
		canonical: map[string]provider.Repo{"tester/old-name": renamed},
	}
}

func TestCollectFollowsRenamedRepository(t *testing.T) {
	root := t.TempDir()
	path := repoWithOrigin(t, root, "old", "https://fake.test/tester/old-name.git")
	prov := renamedProvider()

	pairs, inv := collected(t, prov, root)

	t.Run("paired with new name", func(t *testing.T) {
		assert.Equal(t, []string{"origin tester/new-name"}, pairNames(pairs))
	})
	t.Run("origin name kept", func(t *testing.T) {
		require.Len(t, pairs, 1)
		assert.Equal(t, "tester/old-name", pairs[0].originName)
	})
	t.Run("nothing left over", func(t *testing.T) {
		assert.Equal(t, 0, len(inv.localOnly)+len(inv.remoteOnly))
	})
	t.Run("mismatch recorded", func(t *testing.T) {
		assert.Equal(t, []OriginMismatch{{Path: path, Origin: "tester/old-name", Canonical: "tester/new-name"}}, inv.mismatched)
	})
	t.Run("one lookup", func(t *testing.T) {
		assert.Equal(t, []string{"tester/old-name"}, prov.lookups)
	})
}

func TestCollectKeepsMissingRepositoryLocalOnly(t *testing.T) {
	root := t.TempDir()
	repoWithOrigin(t, root, "gone", "https://fake.test/tester/gone.git")

	pairs, inv := collected(t, &fakeProvider{}, root)

	t.Run("not paired", func(t *testing.T) {
		assert.Empty(t, pairs)
	})
	t.Run("missing remote", func(t *testing.T) {
		assert.Equal(t, []string{KindMissingRemote}, localKinds(inv.localOnly))
	})
	t.Run("no mismatch", func(t *testing.T) {
		assert.Empty(t, inv.mismatched)
	})
}

func TestCollectRemoteOnlyCountsRenamedRepoOnce(t *testing.T) {
	root := t.TempDir()
	repoWithOrigin(t, root, "old", "https://fake.test/tester/old-name.git")

	_, inv := collected(t, renamedProvider(provider.Repo{Owner: "tester", Name: "cloud", DefaultBranch: "main"}), root)

	assert.Equal(t, []string{"tester/cloud"}, remoteNames(inv.remoteOnly))
}

func TestCollectKeepsNonOriginRemoteLocalOnly(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://gitlab.example/x/tool.git")
	git(t, dir, "remote", "add", "github", "https://fake.test/tester/tool.git")
	prov := &fakeProvider{repos: []provider.Repo{{Owner: "tester", Name: "tool", DefaultBranch: "main"}}}

	pairs, inv := collected(t, prov, root)

	t.Run("not paired", func(t *testing.T) {
		assert.Empty(t, pairs)
	})
	t.Run("non-origin kind", func(t *testing.T) {
		assert.Equal(t, []string{KindNonOrigin}, localKinds(inv.localOnly))
	})
	t.Run("not an orphan", func(t *testing.T) {
		require.Len(t, inv.localOnly, 1)
		assert.False(t, inv.localOnly[0].Orphan())
	})
	t.Run("remote seen", func(t *testing.T) {
		assert.Empty(t, inv.remoteOnly)
	})
}

func TestCollectPrefersOriginOverOtherRemotes(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://fake.test/tester/tool.git")
	git(t, dir, "remote", "add", "gitea", "https://fake.test/tester/mirror.git")
	prov := &fakeProvider{repos: []provider.Repo{
		{Owner: "tester", Name: "tool", DefaultBranch: "main"},
		{Owner: "tester", Name: "mirror", DefaultBranch: "main"},
	}}

	pairs, inv := collected(t, prov, root)

	t.Run("paired through origin", func(t *testing.T) {
		assert.Equal(t, []string{"origin tester/tool"}, pairNames(pairs))
	})
	t.Run("no local only", func(t *testing.T) {
		assert.Empty(t, inv.localOnly)
	})
	t.Run("mirror stays remote only", func(t *testing.T) {
		assert.Equal(t, []string{"tester/mirror"}, remoteNames(inv.remoteOnly))
	})
}

func TestCollectKeepsOriginKindWhenNoRemoteMatches(t *testing.T) {
	root := t.TempDir()
	dir := repoWithOrigin(t, root, "tool", "https://gitlab.example/x/tool.git")
	git(t, dir, "remote", "add", "github", "https://fake.test/tester/gone.git")

	_, inv := collected(t, &fakeProvider{}, root)

	assert.Equal(t, []string{KindForeignRemote}, localKinds(inv.localOnly))
}
