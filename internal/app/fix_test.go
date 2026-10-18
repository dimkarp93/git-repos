package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dimkarp93/git-repos/internal/provider"
)

func remoteFact(name string, recent []string, root string) *remoteFacts {
	h := provider.History{Total: len(recent)}
	for _, sha := range recent {
		h.Recent = append(h.Recent, provider.Commit{SHA: sha, Subject: "msg " + sha})
	}
	h.Root = provider.Commit{SHA: root, Subject: "init"}
	repo := provider.Repo{Owner: "tester", Name: name, DefaultBranch: "main"}
	return &remoteFacts{item: RemoteOnly{FullName: repo.FullName(), repo: repo}, history: h}
}

func localFact(path string, commits []string, roots []string, blobs map[string]string) *localFacts {
	f := &localFacts{item: LocalOnly{Path: path, Reason: "no origin"}, commits: map[string]bool{}, roots: map[string]bool{}, blobs: blobs}
	for _, sha := range commits {
		f.commits[sha] = true
	}
	for _, sha := range roots {
		f.roots[sha] = true
	}
	return f
}

type evView struct {
	Root        string
	Shared      []string
	RemoteAhead int
	SameName    bool
	SameFiles   int
	TotalFiles  int
}

func viewEvidence(ev evidence) evView {
	v := evView{RemoteAhead: ev.remoteAhead, SameName: ev.sameName, SameFiles: ev.sameFiles, TotalFiles: ev.totalFiles}
	if ev.sharedRoot != nil {
		v.Root = ev.sharedRoot.SHA
	}
	for _, c := range ev.shared {
		v.Shared = append(v.Shared, c.SHA)
	}
	return v
}

func TestScoreMatchSharedHistory(t *testing.T) {
	local := localFact("/w/tool", []string{"c1", "c2", "c3", "l4"}, []string{"c1"}, nil)
	ev := scoreMatch(local, remoteFact("tool-cli", []string{"r5", "c3", "c2", "c1"}, "c1"))

	t.Run("evidence", func(t *testing.T) {
		assert.Equal(t, evView{Root: "c1", Shared: []string{"c3", "c2", "c1"}, RemoteAhead: 1}, viewEvidence(ev))
	})
	t.Run("matches", func(t *testing.T) {
		assert.True(t, ev.matches(0.5))
	})
}

func TestScoreMatchSharedCommitsWithoutRoot(t *testing.T) {
	local := localFact("/w/tool", []string{"c7", "c8"}, []string{"x0"}, nil)
	remote := remoteFact("tool", []string{"c8", "c7"}, "r0")
	remote.history.Total = 300

	ev := scoreMatch(local, remote)

	assert.Equal(t, evView{Shared: []string{"c8", "c7"}, SameName: true}, viewEvidence(ev))
}

func filesOnlyEvidence() evidence {
	blobs := map[string]string{"go.mod": "b1", "main.go": "b2", "a/x.go": "b3", "local.txt": "b4"}
	local := localFact("/w/tool", []string{"l1"}, []string{"l1"}, blobs)
	remote := remoteFact("tool", []string{"r1"}, "r1")
	remote.tree = []provider.TreeEntry{{Path: "go.mod", SHA: "b1"}, {Path: "main.go", SHA: "b2"}, {Path: "a/x.go", SHA: "b3"}, {Path: "other.go", SHA: "b9"}}
	return scoreMatch(local, remote)
}

func TestScoreMatchFilesOnly(t *testing.T) {
	ev := filesOnlyEvidence()

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, evView{RemoteAhead: -1, SameName: true, SameFiles: 3, TotalFiles: 5}, viewEvidence(ev))
	})
	t.Run("threshold", func(t *testing.T) {
		assert.Equal(t, []bool{true, false}, []bool{ev.matches(0.6), ev.matches(0.7)})
	})
	t.Run("sample paths", func(t *testing.T) {
		assert.Equal(t, []string{"go.mod", "main.go", "a/x.go"}, ev.samplePaths)
	})
}

func TestScoreMatchNothingInCommon(t *testing.T) {
	local := localFact("/w/tool", []string{"l1"}, []string{"l1"}, map[string]string{"a": "b1"})
	remote := remoteFact("tool", []string{"r1"}, "r1")
	remote.tree = []provider.TreeEntry{{Path: "a", SHA: "b2"}}

	assert.False(t, scoreMatch(local, remote).matches(0.01))
}

func historyEvidenceText() string {
	remote := remoteFact("tool", []string{"r5", "c3", "c2", "c1", "c0"}, "c0")
	remote.history.Root.Date = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	local := localFact("/w/tool", []string{"c0", "c1", "c2", "c3"}, []string{"c0"}, nil)
	ev := scoreMatch(local, remote)
	ev.localAhead, ev.aheadKnown = 2, true
	return evidenceText(ev)
}

func TestRenderEvidenceForSharedHistory(t *testing.T) {
	text := historyEvidenceText()

	for _, want := range []string{
		"✓ same first commit     c0  2024-03-01  init",
		"✓ 4 of 5 remote commits are in the local history",
		"    c3",
		"    …and 1 more",
		"→ diverged: local has 2 commits more, remote has 1 commit more",
		"why: an identical commit SHA",
	} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, text, want)
		})
	}
}

func TestRenderEvidenceForRootOnly(t *testing.T) {
	remote := remoteFact("tool", []string{"r5"}, "c0")
	remote.history.Total = 250
	local := localFact("/w/tool", []string{"c0"}, []string{"c0"}, nil)

	text := evidenceText(scoreMatch(local, remote))

	for _, want := range []string{"none of the latest 1 remote commits", "same start"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, text, want)
		})
	}
}

func TestRenderEvidenceForFiles(t *testing.T) {
	ev := evidence{remote: remoteFact("tool", nil, ""), sameFiles: 8, totalFiles: 10, similarity: 0.8, samplePaths: []string{"go.mod"}}

	text := evidenceText(ev)

	for _, want := range []string{"≈ 80% identical files (8 of 10)", "same content: go.mod"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, text, want)
		})
	}
}

func evidenceText(ev evidence) string {
	var b strings.Builder
	for _, line := range renderEvidence(ev) {
		b.WriteString(line.text + "\n")
	}
	return b.String()
}

func TestSharedSummary(t *testing.T) {
	cases := []struct {
		shared, recent, total int
		want                  string
	}{
		{3, 3, 3, "all 3 remote commits are in the local history"},
		{2, 3, 3, "2 of 3 remote commits are in the local history"},
		{40, 100, 512, "40 of the latest 100 remote commits (of 512) are in the local history"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, sharedSummary(tc.shared, tc.recent, tc.total))
		})
	}
}

func TestAutoPick(t *testing.T) {
	root := &provider.Commit{SHA: "r"}
	cases := map[string]struct {
		found []evidence
		want  bool
	}{
		"single candidate":       {found: []evidence{{}}, want: true},
		"only one shared root":   {found: []evidence{{sharedRoot: root}, {}}, want: true},
		"two shared roots":       {found: []evidence{{sharedRoot: root}, {sharedRoot: root}}},
		"ambiguous file matches": {found: []evidence{{similarity: 0.9}, {similarity: 0.8}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, ok := autoPick(tc.found)
			assert.Equal(t, tc.want, ok)
		})
	}
}

func TestRankEvidence(t *testing.T) {
	root := &provider.Commit{SHA: "r"}
	found := []evidence{
		{remote: remoteFact("files", nil, ""), similarity: 0.9},
		{remote: remoteFact("many", nil, ""), shared: make([]provider.Commit, 5)},
		{remote: remoteFact("root", nil, ""), sharedRoot: root, shared: make([]provider.Commit, 1)},
	}

	rankEvidence(found)

	got := []string{found[0].remote.item.repo.Name, found[1].remote.item.repo.Name, found[2].remote.item.repo.Name}
	assert.Equal(t, []string{"root", "many", "files"}, got)
}

func TestCommitDate(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		assert.Equal(t, "          ", commitDate(provider.Commit{}))
	})
	t.Run("known", func(t *testing.T) {
		assert.Equal(t, "2024-03-01", commitDate(provider.Commit{Date: time.Date(2024, 3, 1, 12, 0, 0, 0, time.Local)}))
	})
}

type fixFixture struct {
	root     string
	upstream string
	prov     *fakeProvider
	repo     provider.Repo
	last     fixStats
}

func newFixFixture(t *testing.T, remoteName string) *fixFixture {
	t.Helper()
	root := t.TempDir()
	upstream := filepath.Join(t.TempDir(), "upstream")
	require.NoError(t, os.MkdirAll(upstream, 0o755))
	git(t, upstream, "init", "--initial-branch=main", "--quiet")
	commit(t, upstream, "a.txt")
	commit(t, upstream, "b.txt")
	bare := filepath.Join(t.TempDir(), remoteName+".git")
	git(t, filepath.Dir(bare), "clone", "--bare", "--quiet", upstream, bare)

	repo := provider.Repo{Owner: "tester", Name: remoteName, DefaultBranch: "main", CloneURL: bare, SSHURL: bare}
	prov := &fakeProvider{
		repos:     []provider.Repo{repo},
		histories: map[string]provider.History{repo.FullName(): historyOf(t, upstream)},
	}
	return &fixFixture{root: root, upstream: upstream, prov: prov, repo: repo}
}

func historyOf(t *testing.T, dir string) provider.History {
	t.Helper()
	var h provider.History
	for _, line := range strings.Split(git(t, dir, "log", "--format=%H%x09%s", "main"), "\n") {
		sha, subject, _ := strings.Cut(line, "\t")
		h.Recent = append(h.Recent, provider.Commit{SHA: sha, Subject: subject})
	}
	h.Root = h.Recent[len(h.Recent)-1]
	h.Total = len(h.Recent)
	return h
}

func (f *fixFixture) localCopy(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(f.root, name)
	git(t, f.root, "clone", "--quiet", f.upstream, dir)
	git(t, dir, "remote", "remove", "origin")
	commit(t, dir, "local.txt")
	return dir
}

func (f *fixFixture) run(t *testing.T, input string, opts options) string {
	t.Helper()
	var out bytes.Buffer
	s := newSession(t, f.prov, []string{f.root}, &out)
	if opts.jobs == 0 {
		opts.jobs = 2
	}
	if opts.minSimilarity == 0 {
		opts.minSimilarity = defaultMinSimilarity
	}
	s.opts = opts
	_, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	require.NoError(t, err)
	matches := s.findMatches(s.localFacts(inv.localOnly, inv.remotes, opts), s.remoteFacts(inv.remoteOnly, opts), opts)
	c := newScriptedConfirmer(strings.NewReader(input), &out, opts.assumeYes)
	st := s.linkMatches(matches, c, opts)
	if !st.stopped {
		s.guard, err = newGuard(s.ctx, append(append([]matched{}, inv.pairs...), st.linkedPairs...))
		require.NoError(t, err)
		s.syncNames(append(st.linkedPairs, differentNames(inv.pairs)...), inv.remotes, c, opts, &st)
	}
	f.last = st
	return out.String()
}

type fixCounts struct {
	linked, skipped, failed, renamed int
}

func (f *fixFixture) counts() fixCounts {
	return fixCounts{linked: f.last.linked, skipped: f.last.skipped, failed: f.last.failed, renamed: f.last.renamed}
}

func TestFixAddsOriginWhenMissing(t *testing.T) {
	f := newFixFixture(t, "tool-cli")
	dir := f.localCopy(t, "tool")
	out := f.run(t, "1\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1}, f.counts())
	})
	t.Run("origin", func(t *testing.T) {
		assert.Equal(t, f.repo.CloneURL, git(t, dir, "remote", "get-url", "origin"))
	})
	t.Run("origin head", func(t *testing.T) {
		assert.Equal(t, "origin/main", git(t, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"))
	})
	for _, want := range []string{"tester/tool-cli", "(name differs)", "✓ same first commit", "all 2 remote commits are in the local history", "local is 1 commit ahead"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out, want)
		})
	}
}

func fixWithOrigin(t *testing.T, local, origin string, extra ...[2]string) (*fixFixture, string) {
	t.Helper()

	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, local)
	git(t, dir, "remote", "add", "origin", origin)
	for _, r := range extra {
		git(t, dir, "remote", "add", r[0], r[1])
	}
	return f, dir
}

func TestFixExistingRemoteNameRenamed(t *testing.T) {
	f, dir := fixWithOrigin(t, "tool", "https://gitlab.example/x/tool.git", [2]string{"fake", "https://gitlab.example/x/mirror.git"})
	out := f.run(t, "1\n\nr\nmine\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1}, f.counts())
	})
	for _, want := range []string{"remote fake already exists (https://gitlab.example/x/mirror.git)", "[overwrite | rename]"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out, want)
		})
	}
	t.Run("new remote", func(t *testing.T) {
		assert.Equal(t, f.repo.CloneURL, git(t, dir, "remote", "get-url", "mine"))
	})
	t.Run("existing remote kept", func(t *testing.T) {
		assert.Equal(t, "https://gitlab.example/x/mirror.git", git(t, dir, "remote", "get-url", "fake"))
	})
}

func TestFixExistingRemoteNameOverwritten(t *testing.T) {
	f, dir := fixWithOrigin(t, "tool", "https://gitlab.example/x/tool.git")
	out := f.run(t, "1\norigin\noverwrite\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1}, f.counts())
	})
	t.Run("origin", func(t *testing.T) {
		assert.Equal(t, f.repo.CloneURL, git(t, dir, "remote", "get-url", "origin"))
	})
	t.Run("reported", func(t *testing.T) {
		assert.Contains(t, out, "remote origin overwritten")
	})
}

func TestFixExistingRemoteNameWithYesFails(t *testing.T) {
	f, _ := fixWithOrigin(t, "tool", "https://gitlab.example/x/tool.git", [2]string{"fake", "https://gitlab.example/x/mirror.git"})
	out := f.run(t, "", options{assumeYes: true})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{failed: 1}, f.counts())
	})
	t.Run("hint", func(t *testing.T) {
		assert.Contains(t, out, "run without --yes to overwrite")
	})
}

const deadOrigin = "https://fake.test/tester/tool-cli.git"

func TestFixRemovesDeadOrigin(t *testing.T) {
	f, dir := fixWithOrigin(t, "tool-cli", deadOrigin)
	out := f.run(t, "1\nremove\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1}, f.counts())
	})
	t.Run("asks about the dead remote", func(t *testing.T) {
		assert.Contains(t, out, "remote origin → "+deadOrigin+": tester/tool-cli does not exist on fake")
	})
	t.Run("no name question", func(t *testing.T) {
		assert.NotContains(t, out, "Name of the new remote")
	})
	t.Run("origin replaced", func(t *testing.T) {
		assert.Equal(t, []string{"origin", f.repo.CloneURL}, []string{git(t, dir, "remote"), git(t, dir, "remote", "get-url", "origin")})
	})
}

func TestFixKeepsDeadOrigin(t *testing.T) {
	f, dir := fixWithOrigin(t, "tool-cli", deadOrigin)
	f.run(t, "1\nkeep\n\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, 1, f.counts().linked)
	})
	t.Run("dead origin kept", func(t *testing.T) {
		assert.Equal(t, deadOrigin, git(t, dir, "remote", "get-url", "origin"))
	})
	t.Run("provider remote added", func(t *testing.T) {
		assert.Equal(t, f.repo.CloneURL, git(t, dir, "remote", "get-url", "fake"))
	})
}

func TestFixYesKeepsDeadOrigin(t *testing.T) {
	f, dir := fixWithOrigin(t, "tool-cli", deadOrigin)
	out := f.run(t, "", options{assumeYes: true})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, 1, f.counts().linked)
	})
	t.Run("reported", func(t *testing.T) {
		assert.Contains(t, out, "kept remote origin")
	})
	t.Run("dead origin kept", func(t *testing.T) {
		assert.Equal(t, deadOrigin, git(t, dir, "remote", "get-url", "origin"))
	})
}

func TestFixDefaultRemoteNameIsProvider(t *testing.T) {
	f, dir := fixWithOrigin(t, "tool", "https://gitlab.example/x/tool.git")
	f.run(t, "1\n\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, 1, f.counts().linked)
	})
	t.Run("named after provider", func(t *testing.T) {
		assert.Equal(t, f.repo.CloneURL, git(t, dir, "remote", "get-url", "fake"))
	})
}

func TestFixDryRunChangesNothing(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool")
	out := f.run(t, "", options{dryRun: true})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{}, f.counts())
	})
	t.Run("plan", func(t *testing.T) {
		assert.Contains(t, out, "would add remote origin → tester/tool")
	})
	t.Run("no remotes", func(t *testing.T) {
		assert.Empty(t, git(t, dir, "remote"))
	})
}

func TestFixOfferedRemoteIsNotReused(t *testing.T) {
	f := newFixFixture(t, "tool")
	first := f.localCopy(t, "one")
	second := f.localCopy(t, "two")
	out := f.run(t, "1\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, 1, f.counts().linked)
	})
	t.Run("offered once", func(t *testing.T) {
		assert.Equal(t, 1, strings.Count(out, "1) tester/tool"))
	})
	t.Run("remotes", func(t *testing.T) {
		assert.Equal(t, []string{"origin", ""}, []string{git(t, first, "remote"), git(t, second, "remote")})
	})
}

func TestFixYesSkipsAmbiguous(t *testing.T) {
	f := newFixFixture(t, "tool")
	f.localCopy(t, "tool")
	other := f.repo
	other.Name = "tool-copy"
	f.prov.repos = append(f.prov.repos, other)
	f.prov.histories[other.FullName()] = f.prov.histories[f.repo.FullName()]
	out := f.run(t, "", options{assumeYes: true})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{skipped: 1}, f.counts())
	})
	t.Run("reported", func(t *testing.T) {
		assert.Contains(t, out, "ambiguous")
	})
}

func freshCopyFixture(t *testing.T) *fixFixture {
	t.Helper()

	f := newFixFixture(t, "tool")
	dir := filepath.Join(f.root, "fresh")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	git(t, dir, "init", "--initial-branch=main", "--quiet")
	for _, name := range []string{"a.txt", "b.txt"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644))
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "fresh start")

	f.prov.trees = map[string][]provider.TreeEntry{"tester/tool": {
		{Path: "a.txt", SHA: git(t, f.upstream, "rev-parse", "main:a.txt")},
		{Path: "b.txt", SHA: git(t, f.upstream, "rev-parse", "main:b.txt")},
	}}
	return f
}

func TestFixMatchesByFilesWithoutHistory(t *testing.T) {
	f := freshCopyFixture(t)
	out := f.run(t, "1\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, 1, f.counts().linked)
	})
	t.Run("evidence", func(t *testing.T) {
		assert.Contains(t, out, "≈ 100% identical files (2 of 2)")
	})
	t.Run("trees read once", func(t *testing.T) {
		assert.Len(t, f.prov.treeCalls, 1)
	})
}

func TestFixRenamesLocalDirToRemoteName(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	out := f.run(t, "1\nlocal\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1, renamed: 1}, f.counts())
	})
	for _, want := range []string{"Names differ:", "rename the directory", "[local | remote | keep]"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out, want)
		})
	}
	t.Run("old dir gone", func(t *testing.T) {
		assert.NoDirExists(t, dir)
	})
	t.Run("renamed dir linked", func(t *testing.T) {
		assert.Equal(t, f.repo.CloneURL, git(t, filepath.Join(f.root, "tool"), "remote", "get-url", "origin"))
	})
}

func TestFixRenamesRemoteToLocalName(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	out := f.run(t, "1\nremote\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1, renamed: 1}, f.counts())
	})
	t.Run("remote renamed", func(t *testing.T) {
		assert.Equal(t, []string{"tester/tool->tool-cli"}, f.prov.renamed)
	})
	t.Run("local dir kept", func(t *testing.T) {
		assert.DirExists(t, dir)
	})
	t.Run("reported", func(t *testing.T) {
		assert.Contains(t, out, "remote origin updated")
	})
}

func TestFixSkipsRenameForRepositoryWithWorktree(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	git(t, dir, "worktree", "add", "-b", "feature", filepath.Join(t.TempDir(), "feature"))
	out := f.run(t, "1\nlocal\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1, skipped: 1}, f.counts())
	})
	t.Run("blocked", func(t *testing.T) {
		assert.Len(t, f.last.blocked, 1)
	})
	t.Run("rename not offered", func(t *testing.T) {
		assert.NotContains(t, out, "rename the directory")
	})
	t.Run("directory kept", func(t *testing.T) {
		assert.DirExists(t, dir)
	})
}

func conflictingNamesFixture(t *testing.T) (*fixFixture, string) {
	t.Helper()

	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(f.root, "tool"), 0o755))
	f.prov.repos = append(f.prov.repos, provider.Repo{Owner: "tester", Name: "tool-cli"})

	return f, dir
}

func TestFixRenameConflictAsksAgain(t *testing.T) {
	f, dir := conflictingNamesFixture(t)
	out := f.run(t, "1\nlocal\nremote\nkeep\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{linked: 1, skipped: 1}, f.counts())
	})
	t.Run("asked again", func(t *testing.T) {
		assert.Equal(t, 3, strings.Count(out, "Rename which one?"))
	})
	for _, want := range []string{"already exists, pick another option", "tester/tool-cli already exists on fake"} {
		t.Run(want, func(t *testing.T) {
			assert.Contains(t, out, want)
		})
	}
	t.Run("local dir kept", func(t *testing.T) {
		assert.DirExists(t, dir)
	})
}

func TestFixOffersRenameForLinkedPair(t *testing.T) {
	f, _ := fixWithOrigin(t, "old-name", "https://fake.test/tester/tool.git")
	f.run(t, "local\n", options{})

	t.Run("counts", func(t *testing.T) {
		assert.Equal(t, fixCounts{renamed: 1}, f.counts())
	})
	t.Run("renamed dir", func(t *testing.T) {
		assert.DirExists(t, filepath.Join(f.root, "tool"))
	})
}

func TestFixNamesDifferWithYes(t *testing.T) {
	f, dir := fixWithOrigin(t, "old-name", "https://fake.test/tester/tool.git")
	out := f.run(t, "", options{assumeYes: true})

	t.Run("skipped", func(t *testing.T) {
		assert.Equal(t, 1, f.counts().skipped)
	})
	t.Run("hint", func(t *testing.T) {
		assert.Contains(t, out, "run without --yes to choose")
	})
	t.Run("dir kept", func(t *testing.T) {
		assert.DirExists(t, dir)
	})
}

func TestFixNamesDifferDryRun(t *testing.T) {
	f, dir := fixWithOrigin(t, "old-name", "https://fake.test/tester/tool.git")
	out := f.run(t, "", options{dryRun: true})

	t.Run("counted", func(t *testing.T) {
		assert.Equal(t, 1, f.last.namesDiffer)
	})
	t.Run("plan", func(t *testing.T) {
		assert.Contains(t, out, "would ask which one to rename")
	})
	t.Run("nothing renamed", func(t *testing.T) {
		assert.DirExists(t, dir)
		assert.Empty(t, f.prov.renamed)
	})
}
