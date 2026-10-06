package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestScoreMatchSharedHistory(t *testing.T) {
	local := localFact("/w/tool", []string{"c1", "c2", "c3", "l4"}, []string{"c1"}, nil)
	remote := remoteFact("tool-cli", []string{"r5", "c3", "c2", "c1"}, "c1")
	ev := scoreMatch(local, remote)
	if ev.sharedRoot == nil || ev.sharedRoot.SHA != "c1" {
		t.Fatalf("sharedRoot = %+v", ev.sharedRoot)
	}
	if len(ev.shared) != 3 || ev.shared[0].SHA != "c3" || ev.remoteAhead != 1 {
		t.Fatalf("shared = %+v, remoteAhead = %d", ev.shared, ev.remoteAhead)
	}
	if ev.sameName || !ev.matches(0.5) {
		t.Fatalf("ev = %+v", ev)
	}
}

func TestScoreMatchSharedCommitsWithoutRoot(t *testing.T) {
	local := localFact("/w/tool", []string{"c7", "c8"}, []string{"x0"}, nil)
	remote := remoteFact("tool", []string{"c8", "c7"}, "r0")
	remote.history.Total = 300
	ev := scoreMatch(local, remote)
	if ev.sharedRoot != nil || len(ev.shared) != 2 || ev.remoteAhead != 0 || !ev.sameName {
		t.Fatalf("ev = %+v", ev)
	}
}

func TestScoreMatchFilesOnly(t *testing.T) {
	blobs := map[string]string{"go.mod": "b1", "main.go": "b2", "a/x.go": "b3", "local.txt": "b4"}
	local := localFact("/w/tool", []string{"l1"}, []string{"l1"}, blobs)
	remote := remoteFact("tool", []string{"r1"}, "r1")
	remote.tree = []provider.TreeEntry{{Path: "go.mod", SHA: "b1"}, {Path: "main.go", SHA: "b2"}, {Path: "a/x.go", SHA: "b3"}, {Path: "other.go", SHA: "b9"}}
	ev := scoreMatch(local, remote)
	if ev.byHistory() || ev.sameFiles != 3 || ev.totalFiles != 5 {
		t.Fatalf("ev = %+v", ev)
	}
	if !ev.matches(0.6) || ev.matches(0.7) {
		t.Fatalf("similarity = %v", ev.similarity)
	}
	if strings.Join(ev.samplePaths, ",") != "go.mod,main.go,a/x.go" {
		t.Fatalf("samplePaths = %v", ev.samplePaths)
	}
}

func TestScoreMatchNothingInCommon(t *testing.T) {
	local := localFact("/w/tool", []string{"l1"}, []string{"l1"}, map[string]string{"a": "b1"})
	remote := remoteFact("tool", []string{"r1"}, "r1")
	remote.tree = []provider.TreeEntry{{Path: "a", SHA: "b2"}}
	if ev := scoreMatch(local, remote); ev.matches(0.01) {
		t.Fatalf("ev = %+v", ev)
	}
}

func TestRenderEvidence(t *testing.T) {
	remote := remoteFact("tool", []string{"r5", "c3", "c2", "c1", "c0"}, "c0")
	remote.history.Root.Date = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	local := localFact("/w/tool", []string{"c0", "c1", "c2", "c3"}, []string{"c0"}, nil)
	ev := scoreMatch(local, remote)
	ev.localAhead, ev.aheadKnown = 2, true
	text := evidenceText(ev)
	for _, want := range []string{
		"✓ same first commit     c0  2024-03-01  init",
		"✓ 4 of 5 remote commits are in the local history",
		"    c3",
		"    …and 1 more",
		"→ diverged: local has 2 commits more, remote has 1 commit more",
		"why: an identical commit SHA",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}

	remote.history.Recent = remote.history.Recent[:1]
	remote.history.Total = 250
	text = evidenceText(scoreMatch(local, remote))
	if !strings.Contains(text, "none of the latest 1 remote commits") || !strings.Contains(text, "same start") {
		t.Errorf("root only:\n%s", text)
	}

	files := evidence{remote: remote, sameFiles: 8, totalFiles: 10, similarity: 0.8, samplePaths: []string{"go.mod"}}
	text = evidenceText(files)
	if !strings.Contains(text, "≈ 80% identical files (8 of 10)") || !strings.Contains(text, "same content: go.mod") {
		t.Errorf("files:\n%s", text)
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
		if got := sharedSummary(tc.shared, tc.recent, tc.total); got != tc.want {
			t.Errorf("sharedSummary(%d, %d, %d) = %q", tc.shared, tc.recent, tc.total, got)
		}
	}
}

func TestAutoPick(t *testing.T) {
	root := &provider.Commit{SHA: "r"}
	one := []evidence{{}}
	if _, ok := autoPick(one); !ok {
		t.Error("single candidate not picked")
	}
	if _, ok := autoPick([]evidence{{sharedRoot: root}, {}}); !ok {
		t.Error("the only candidate with a shared root not picked")
	}
	if _, ok := autoPick([]evidence{{sharedRoot: root}, {sharedRoot: root}}); ok {
		t.Error("ambiguous candidates picked")
	}
	if _, ok := autoPick([]evidence{{similarity: 0.9}, {similarity: 0.8}}); ok {
		t.Error("ambiguous file matches picked")
	}
}

func TestRankEvidence(t *testing.T) {
	root := &provider.Commit{SHA: "r"}
	mk := func(name string) *remoteFacts { return remoteFact(name, nil, "") }
	found := []evidence{
		{remote: mk("files"), similarity: 0.9},
		{remote: mk("many"), shared: make([]provider.Commit, 5)},
		{remote: mk("root"), sharedRoot: root, shared: make([]provider.Commit, 1)},
	}
	rankEvidence(found)
	got := []string{found[0].remote.item.repo.Name, found[1].remote.item.repo.Name, found[2].remote.item.repo.Name}
	if strings.Join(got, ",") != "root,many,files" {
		t.Fatalf("order = %v", got)
	}
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
	if err := os.MkdirAll(upstream, 0o755); err != nil {
		t.Fatal(err)
	}
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

func (f *fixFixture) run(t *testing.T, input string, opts options) (string, int, int, int) {
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
	if err != nil {
		t.Fatal(err)
	}
	matches := s.findMatches(s.localFacts(inv.localOnly, inv.remotes, opts), s.remoteFacts(inv.remoteOnly, opts), opts)
	c := newScriptedConfirmer(strings.NewReader(input), &out, opts.assumeYes)
	st := s.linkMatches(matches, c, opts)
	if !st.stopped {
		s.syncNames(append(st.linkedPairs, differentNames(inv.pairs)...), inv.remotes, c, opts, &st)
	}
	f.last = st
	return out.String(), st.linked, st.skipped, st.failed
}

func TestFixAddsOriginWhenMissing(t *testing.T) {
	f := newFixFixture(t, "tool-cli")
	dir := f.localCopy(t, "tool")

	out, linked, _, failed := f.run(t, "1\n", options{})
	if linked != 1 || failed != 0 {
		t.Fatalf("linked = %d, failed = %d\n%s", linked, failed, out)
	}
	if got := git(t, dir, "remote", "get-url", "origin"); got != f.repo.CloneURL {
		t.Fatalf("origin = %q", got)
	}
	if got := git(t, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); got != "origin/main" {
		t.Fatalf("origin/HEAD = %q", got)
	}
	for _, want := range []string{"tester/tool-cli", "(name differs)", "✓ same first commit", "all 2 remote commits are in the local history", "local is 1 commit ahead"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestFixExistingRemoteNameRenamed(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool")
	git(t, dir, "remote", "add", "origin", "https://gitlab.example/x/tool.git")
	git(t, dir, "remote", "add", "fake", "https://gitlab.example/x/mirror.git")

	out, linked, _, failed := f.run(t, "1\n\nr\nmine\n", options{})
	if linked != 1 || failed != 0 {
		t.Fatalf("linked = %d, failed = %d\n%s", linked, failed, out)
	}
	if !strings.Contains(out, "remote fake already exists (https://gitlab.example/x/mirror.git)") || !strings.Contains(out, "[overwrite | rename]") {
		t.Errorf("no overwrite question:\n%s", out)
	}
	if got := git(t, dir, "remote", "get-url", "mine"); got != f.repo.CloneURL {
		t.Fatalf("mine = %q", got)
	}
	if got := git(t, dir, "remote", "get-url", "fake"); got != "https://gitlab.example/x/mirror.git" {
		t.Fatalf("fake changed to %q", got)
	}
}

func TestFixExistingRemoteNameOverwritten(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool")
	git(t, dir, "remote", "add", "origin", "https://gitlab.example/x/tool.git")

	out, linked, _, failed := f.run(t, "1\norigin\noverwrite\n", options{})
	if linked != 1 || failed != 0 {
		t.Fatalf("linked = %d, failed = %d\n%s", linked, failed, out)
	}
	if got := git(t, dir, "remote", "get-url", "origin"); got != f.repo.CloneURL {
		t.Fatalf("origin = %q", got)
	}
	if !strings.Contains(out, "remote origin overwritten") {
		t.Errorf("output:\n%s", out)
	}
}

func TestFixExistingRemoteNameWithYesFails(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool")
	git(t, dir, "remote", "add", "origin", "https://gitlab.example/x/tool.git")
	git(t, dir, "remote", "add", "fake", "https://gitlab.example/x/mirror.git")

	out, linked, _, failed := f.run(t, "", options{assumeYes: true})
	if linked != 0 || failed != 1 || !strings.Contains(out, "run without --yes to overwrite") {
		t.Fatalf("linked = %d, failed = %d\n%s", linked, failed, out)
	}
}

func TestFixRemovesDeadOrigin(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/tool-cli.git")

	out, linked, _, failed := f.run(t, "1\nremove\n", options{})
	if linked != 1 || failed != 0 {
		t.Fatalf("linked = %d, failed = %d\n%s", linked, failed, out)
	}
	if !strings.Contains(out, "remote origin → https://fake.test/tester/tool-cli.git: tester/tool-cli does not exist on fake") {
		t.Errorf("no dead remote question:\n%s", out)
	}
	if strings.Contains(out, "Name of the new remote") {
		t.Errorf("name asked although origin was freed:\n%s", out)
	}
	if got := git(t, dir, "remote"); got != "origin" {
		t.Fatalf("remotes = %q", got)
	}
	if got := git(t, dir, "remote", "get-url", "origin"); got != f.repo.CloneURL {
		t.Fatalf("origin = %q", got)
	}
}

func TestFixKeepsDeadOrigin(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/tool-cli.git")

	out, linked, _, _ := f.run(t, "1\nkeep\n\n", options{})
	if linked != 1 {
		t.Fatalf("linked = %d\n%s", linked, out)
	}
	if got := git(t, dir, "remote", "get-url", "origin"); got != "https://fake.test/tester/tool-cli.git" {
		t.Fatalf("origin = %q", got)
	}
	if got := git(t, dir, "remote", "get-url", "fake"); got != f.repo.CloneURL {
		t.Fatalf("fake = %q", got)
	}
}

func TestFixYesKeepsDeadOrigin(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/tool-cli.git")

	out, linked, _, _ := f.run(t, "", options{assumeYes: true})
	if linked != 1 || !strings.Contains(out, "kept remote origin") {
		t.Fatalf("linked = %d\n%s", linked, out)
	}
	if got := git(t, dir, "remote", "get-url", "origin"); got != "https://fake.test/tester/tool-cli.git" {
		t.Fatalf("origin = %q", got)
	}
}

func TestFixDefaultRemoteNameIsProvider(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool")
	git(t, dir, "remote", "add", "origin", "https://gitlab.example/x/tool.git")

	out, linked, _, _ := f.run(t, "1\n\n", options{})
	if linked != 1 {
		t.Fatalf("linked = %d\n%s", linked, out)
	}
	if got := git(t, dir, "remote", "get-url", "fake"); got != f.repo.CloneURL {
		t.Fatalf("fake = %q", got)
	}
}

func TestFixDryRunChangesNothing(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool")

	out, linked, _, _ := f.run(t, "", options{dryRun: true})
	if linked != 0 || !strings.Contains(out, "would add remote origin → tester/tool") {
		t.Fatalf("linked = %d\n%s", linked, out)
	}
	if remotes := git(t, dir, "remote"); remotes != "" {
		t.Fatalf("remotes = %q", remotes)
	}
}

func TestFixOfferedRemoteIsNotReused(t *testing.T) {
	f := newFixFixture(t, "tool")
	first := f.localCopy(t, "one")
	second := f.localCopy(t, "two")

	out, linked, _, _ := f.run(t, "1\n", options{})
	if linked != 1 {
		t.Fatalf("linked = %d\n%s", linked, out)
	}
	if strings.Count(out, "1) tester/tool") != 1 {
		t.Fatalf("remote offered twice:\n%s", out)
	}
	if git(t, first, "remote") != "origin" || git(t, second, "remote") != "" {
		t.Fatalf("remotes: one = %q, two = %q", git(t, first, "remote"), git(t, second, "remote"))
	}
}

func TestFixYesSkipsAmbiguous(t *testing.T) {
	f := newFixFixture(t, "tool")
	f.localCopy(t, "tool")
	other := f.repo
	other.Name = "tool-copy"
	f.prov.repos = append(f.prov.repos, other)
	f.prov.histories[other.FullName()] = f.prov.histories[f.repo.FullName()]

	out, linked, skipped, _ := f.run(t, "", options{assumeYes: true})
	if linked != 0 || skipped != 1 || !strings.Contains(out, "ambiguous") {
		t.Fatalf("linked = %d, skipped = %d\n%s", linked, skipped, out)
	}
}

func TestFixMatchesByFilesWithoutHistory(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := filepath.Join(f.root, "fresh")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "init", "--initial-branch=main", "--quiet")
	for _, name := range []string{"a.txt", "b.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "add", ".")
	git(t, dir, "commit", "--quiet", "-m", "fresh start")
	f.prov.trees = map[string][]provider.TreeEntry{"tester/tool": {
		{Path: "a.txt", SHA: git(t, f.upstream, "rev-parse", "main:a.txt")},
		{Path: "b.txt", SHA: git(t, f.upstream, "rev-parse", "main:b.txt")},
	}}

	out, linked, _, _ := f.run(t, "1\n", options{})
	if linked != 1 || !strings.Contains(out, "≈ 100% identical files (2 of 2)") {
		t.Fatalf("linked = %d\n%s", linked, out)
	}
	if len(f.prov.treeCalls) != 1 {
		t.Fatalf("treeCalls = %v", f.prov.treeCalls)
	}
}

func TestFixRenamesLocalDirToRemoteName(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")

	out, linked, _, failed := f.run(t, "1\nlocal\n", options{})
	if linked != 1 || failed != 0 || f.last.renamed != 1 {
		t.Fatalf("linked = %d, failed = %d, renamed = %d\n%s", linked, failed, f.last.renamed, out)
	}
	for _, want := range []string{"Names differ:", "rename the directory", "[local | remote | keep]"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("old dir still exists: %v", err)
	}
	if got := git(t, filepath.Join(f.root, "tool"), "remote", "get-url", "origin"); got != f.repo.CloneURL {
		t.Fatalf("origin = %q", got)
	}
}

func TestFixRenamesRemoteToLocalName(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")

	out, _, _, failed := f.run(t, "1\nremote\n", options{})
	if failed != 0 || f.last.renamed != 1 {
		t.Fatalf("failed = %d, renamed = %d\n%s", failed, f.last.renamed, out)
	}
	if len(f.prov.renamed) != 1 || f.prov.renamed[0] != "tester/tool->tool-cli" {
		t.Fatalf("renamed = %v", f.prov.renamed)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("local dir moved: %v", err)
	}
	if !strings.Contains(out, "remote origin updated") {
		t.Errorf("output:\n%s", out)
	}
}

func TestFixRenameConflictAsksAgain(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "tool-cli")
	if err := os.MkdirAll(filepath.Join(f.root, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.prov.repos = append(f.prov.repos, provider.Repo{Owner: "tester", Name: "tool-cli"})

	out, _, skipped, failed := f.run(t, "1\nlocal\nremote\nkeep\n", options{})
	if failed != 0 || skipped != 1 || f.last.renamed != 0 {
		t.Fatalf("failed = %d, skipped = %d, renamed = %d\n%s", failed, skipped, f.last.renamed, out)
	}
	if strings.Count(out, "Rename which one?") != 3 {
		t.Errorf("not asked again:\n%s", out)
	}
	if !strings.Contains(out, "already exists, pick another option") || !strings.Contains(out, "tester/tool-cli already exists on fake") {
		t.Errorf("no conflict message:\n%s", out)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("local dir moved: %v", err)
	}
}

func TestFixOffersRenameForLinkedPair(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "old-name")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/tool.git")

	out, linked, _, _ := f.run(t, "local\n", options{})
	if linked != 0 || f.last.renamed != 1 {
		t.Fatalf("linked = %d, renamed = %d\n%s", linked, f.last.renamed, out)
	}
	if _, err := os.Stat(filepath.Join(f.root, "tool")); err != nil {
		t.Fatalf("dir not renamed: %v\n%s", err, out)
	}
}

func TestFixNamesDifferYesAndDryRun(t *testing.T) {
	f := newFixFixture(t, "tool")
	dir := f.localCopy(t, "old-name")
	git(t, dir, "remote", "add", "origin", "https://fake.test/tester/tool.git")

	out, _, skipped, _ := f.run(t, "", options{assumeYes: true})
	if skipped != 1 || !strings.Contains(out, "run without --yes to choose") {
		t.Fatalf("skipped = %d\n%s", skipped, out)
	}
	out, _, _, _ = f.run(t, "", options{dryRun: true})
	if f.last.namesDiffer != 1 || !strings.Contains(out, "would ask which one to rename") {
		t.Fatalf("namesDiffer = %d\n%s", f.last.namesDiffer, out)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("dir moved: %v", err)
	}
	if len(f.prov.renamed) != 0 {
		t.Fatalf("renamed = %v", f.prov.renamed)
	}
}
