package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/git-repos/internal/scan"
)

const (
	defaultMinSimilarity = 0.5
	sharedCommitsShown   = 3
	samePathsShown       = 4
)

type localFacts struct {
	item      LocalOnly
	originURL string
	dead      []deadRemote
	commits   map[string]bool
	roots     map[string]bool
	blobs     map[string]string
}

type deadRemote struct {
	name     string
	url      string
	fullName string
}

type remoteFacts struct {
	item    RemoteOnly
	history provider.History
	tree    []provider.TreeEntry
	treeErr error
}

type evidence struct {
	remote      *remoteFacts
	sharedRoot  *provider.Commit
	shared      []provider.Commit
	remoteAhead int
	localAhead  int
	aheadKnown  bool
	sameFiles   int
	totalFiles  int
	similarity  float64
	samplePaths []string
	sameName    bool
}

func (e evidence) byHistory() bool {
	return e.sharedRoot != nil || len(e.shared) > 0
}

func (e evidence) matches(minSimilarity float64) bool {
	return e.byHistory() || e.sameFiles > 0 && e.similarity >= minSimilarity
}

func fixFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("fix", opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "show the matches and change nothing")
	fs.BoolVar(&opts.assumeYes, "yes", false, "do not ask: link unambiguous matches, name new remotes after the provider")
	fs.StringVar(&opts.protocol, "protocol", "", "remote protocol for new remotes: ssh or https")
	fs.Float64Var(&opts.minSimilarity, "min-similarity", defaultMinSimilarity, "share of identical files to match repositories without common commits, 0..1")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	return fs
}

func runFix(args []string) int {
	opts := options{}
	fs := fixFlags(&opts)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	if err := validateFix(opts); err != nil {
		return fail(err)
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(6)
	s.spinner.Start(s.pr.label)
	_, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		s.spinner.Stop()
		return fail(err)
	}
	candidates := s.fixCandidates(inv, opts)
	s.spinner.Stop()

	differ := differentNames(inv.pairs)
	if len(candidates) == 0 && len(differ) == 0 {
		s.printer.Line(render.Grey, "Nothing to fix: no local-only repository shares history or files with a remote-only one, and linked repositories have the same names.")
		return ExitOK
	}

	stats, err := s.applyFix(inv, candidates, differ, opts)
	if err != nil {
		return fail(err)
	}
	return s.fixSummary(len(candidates), stats, opts)
}

func validateFix(opts options) error {
	if opts.protocol != "" && opts.protocol != provider.ProtocolSSH && opts.protocol != provider.ProtocolHTTPS {
		return errors.New("--protocol accepts ssh or https")
	}
	if opts.minSimilarity <= 0 || opts.minSimilarity > 1 {
		return errors.New("--min-similarity accepts a value above 0 and up to 1")
	}
	return nil
}

func (s *session) fixCandidates(inv inventory, opts options) []localMatches {
	if len(inv.localOnly) == 0 || len(inv.remoteOnly) == 0 {
		return nil
	}

	locals := s.localFacts(inv.localOnly, inv.remotes, opts)
	remotes := s.remoteFacts(inv.remoteOnly, opts)
	return s.findMatches(locals, remotes, opts)
}

func (s *session) applyFix(inv inventory, candidates []localMatches, differ []matched, opts options) (fixStats, error) {
	var c *confirmer
	if !opts.dryRun {
		c = newConfirmer(opts.assumeYes)
	}

	stats := s.linkMatches(candidates, c, opts)
	if stats.stopped {
		return stats, nil
	}

	namePairs := slices.Concat(stats.linkedPairs, differ)
	if len(namePairs) > 0 {
		g, err := newGuard(s.ctx, slices.Concat(inv.pairs, stats.linkedPairs))
		if err != nil {
			return stats, err
		}
		s.guard = g
	}
	s.syncNames(namePairs, inv.remotes, c, opts, &stats)
	return stats, nil
}

func (s *session) fixSummary(candidates int, stats fixStats, opts options) int {
	printSkipped(s.printer, stats.blocked)
	if opts.dryRun {
		s.printer.Line("", "Summary: possible links — %d · names differ — %d", candidates, stats.namesDiffer)
		return ExitOK
	}

	s.printer.Line("", "Summary: linked — %d · renamed — %d · skipped — %d · errors — %d", stats.linked, stats.renamed, stats.skipped, stats.failed)
	if stats.failed > 0 || len(stats.blocked) > 0 {
		return ExitFailure
	}
	return ExitOK
}

type fixStats struct {
	linked      int
	renamed     int
	skipped     int
	failed      int
	namesDiffer int
	stopped     bool
	linkedPairs []matched
	blocked     []skippedRepo
}

func (s *session) localFacts(items []LocalOnly, account []provider.Repo, opts options) []*localFacts {
	existing := make(map[string]bool, len(account))
	for _, repo := range account {
		existing[strings.ToLower(repo.FullName())] = true
	}

	usable := s.usableLocals(items)
	out := make([]*localFacts, len(usable))
	s.pr.setPhase("reading local history")
	s.pr.setUnit("repositories")
	s.pr.setTotal(len(usable))
	parallel(opts.jobs, len(usable), func(i int) {
		s.pr.begin(shortPath(usable[i].Path))
		defer s.pr.end()
		out[i] = s.readLocalFacts(usable[i], existing)
	})
	return out
}

func (s *session) usableLocals(items []LocalOnly) []LocalOnly {
	usable := make([]LocalOnly, 0, len(items))
	for _, item := range items {
		if item.Kind != KindUnreadable && item.Kind != KindNonOrigin && gitcmd.HasCommits(s.ctx, item.Path) {
			usable = append(usable, item)
		}
	}
	return usable
}

func (s *session) readLocalFacts(item LocalOnly, existing map[string]bool) *localFacts {
	facts := &localFacts{item: item, roots: map[string]bool{}, blobs: map[string]string{}}
	facts.originURL, _ = gitcmd.OriginURL(s.ctx, item.Path)
	facts.dead = s.deadRemotes(item.Path, existing)
	facts.commits, _ = gitcmd.AllCommits(s.ctx, item.Path)

	roots, _ := gitcmd.RootCommits(s.ctx, item.Path)
	for _, root := range roots {
		facts.roots[root] = true
	}
	blobs, _ := gitcmd.TreeBlobs(s.ctx, item.Path, "HEAD")
	for _, blob := range blobs {
		facts.blobs[blob.Path] = blob.SHA
	}
	return facts
}

func (s *session) deadRemotes(dir string, existing map[string]bool) []deadRemote {
	remotes, err := gitcmd.Remotes(s.ctx, dir)
	if err != nil {
		return nil
	}
	var dead []deadRemote
	for _, remote := range remotes {
		owner, name, ok := s.prov.ParseRemote(remote.URL)
		if !ok || existing[strings.ToLower(owner+"/"+name)] {
			continue
		}
		if _, err := s.prov.Repo(s.ctx, owner, name); errors.Is(err, provider.ErrNotFound) {
			dead = append(dead, deadRemote{name: remote.Name, url: remote.URL, fullName: owner + "/" + name})
		}
	}
	return dead
}

func (s *session) remoteFacts(items []RemoteOnly, opts options) []*remoteFacts {
	all := make([]*remoteFacts, len(items))
	s.pr.setPhase("reading history on " + s.prov.Name())
	s.pr.setUnit("repositories")
	s.pr.setTotal(len(items))
	parallel(opts.jobs, len(items), func(i int) {
		s.pr.begin(items[i].FullName)
		defer s.pr.end()
		all[i] = s.readRemoteFacts(items[i])
	})

	out := make([]*remoteFacts, 0, len(all))
	for _, facts := range all {
		if facts != nil {
			out = append(out, facts)
		}
	}
	return out
}

func (s *session) readRemoteFacts(item RemoteOnly) *remoteFacts {
	history, err := s.prov.History(s.ctx, item.repo.Owner, item.repo.Name, item.repo.DefaultBranch)
	if err != nil {
		return nil
	}

	return &remoteFacts{item: item, history: history}
}

type localMatches struct {
	local *localFacts
	found []evidence
}

func (s *session) findMatches(locals []*localFacts, remotes []*remoteFacts, opts options) []localMatches {
	byHistory, needFiles := historyMatches(locals, remotes)
	if needFiles {
		s.loadTrees(remotes, opts)
	}

	var out []localMatches
	for i, local := range locals {
		found := byHistory[i]
		if len(found) == 0 {
			found = fileMatches(local, remotes, opts.minSimilarity)
		}
		if len(found) > 0 {
			out = append(out, s.ranked(local, found))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].local.item.Path < out[j].local.item.Path })
	return out
}

func historyMatches(locals []*localFacts, remotes []*remoteFacts) ([][]evidence, bool) {
	byHistory := make([][]evidence, len(locals))
	needFiles := false
	for i, local := range locals {
		for _, remote := range remotes {
			if ev := scoreMatch(local, remote); ev.byHistory() {
				byHistory[i] = append(byHistory[i], ev)
			}
		}
		needFiles = needFiles || len(byHistory[i]) == 0
	}
	return byHistory, needFiles
}

func fileMatches(local *localFacts, remotes []*remoteFacts, minSimilarity float64) []evidence {
	var found []evidence
	for _, remote := range remotes {
		if ev := scoreMatch(local, remote); ev.matches(minSimilarity) {
			found = append(found, ev)
		}
	}
	return found
}

func (s *session) ranked(local *localFacts, found []evidence) localMatches {
	for j := range found {
		s.measureAhead(local, &found[j])
	}

	rankEvidence(found)
	return localMatches{local: local, found: found}
}

func (s *session) loadTrees(remotes []*remoteFacts, opts options) {
	s.pr.setPhase("reading files on " + s.prov.Name())
	s.pr.setUnit("repositories")
	s.pr.setTotal(len(remotes))
	parallel(opts.jobs, len(remotes), func(i int) {
		remote := remotes[i]
		s.pr.begin(remote.item.FullName)
		defer s.pr.end()
		remote.tree, remote.treeErr = s.prov.Tree(s.ctx, remote.item.repo.Owner, remote.item.repo.Name, remote.item.repo.DefaultBranch)
	})
}

func (s *session) measureAhead(local *localFacts, ev *evidence) {
	if len(ev.shared) == 0 {
		return
	}
	n, err := gitcmd.CountCommits(s.ctx, local.item.Path, ev.shared[0].SHA, "HEAD")
	if err == nil {
		ev.localAhead = n
		ev.aheadKnown = true
	}
}

func scoreMatch(local *localFacts, remote *remoteFacts) evidence {
	ev := historyEvidence(local, remote)
	if ev.byHistory() || len(remote.tree) == 0 || len(local.blobs) == 0 {
		return ev
	}

	return withFileEvidence(ev, local, remote)
}

func historyEvidence(local *localFacts, remote *remoteFacts) evidence {
	ev := evidence{
		remote:      remote,
		remoteAhead: -1,
		sameName:    strings.EqualFold(filepath.Base(local.item.Path), remote.item.repo.Name),
	}
	if root := remote.history.Root; root.SHA != "" && (local.roots[root.SHA] || local.commits[root.SHA]) {
		ev.sharedRoot = &root
	}

	for i, commit := range remote.history.Recent {
		if !local.commits[commit.SHA] {
			continue
		}
		if ev.remoteAhead < 0 {
			ev.remoteAhead = i
		}
		ev.shared = append(ev.shared, commit)
	}
	return ev
}

func withFileEvidence(ev evidence, local *localFacts, remote *remoteFacts) evidence {
	localSHAs := map[string]bool{}
	for _, sha := range local.blobs {
		localSHAs[sha] = true
	}
	remoteSHAs := map[string]bool{}
	for _, entry := range remote.tree {
		remoteSHAs[entry.SHA] = true
	}

	common := 0
	for sha := range remoteSHAs {
		if localSHAs[sha] {
			common++
		}
	}
	union := len(localSHAs) + len(remoteSHAs) - common
	if union == 0 || common == 0 {
		return ev
	}

	paths := samePaths(local, remote, localSHAs)
	ev.sameFiles = common
	ev.totalFiles = union
	ev.similarity = float64(common) / float64(union)
	ev.samplePaths = paths[:min(len(paths), samePathsShown)]
	return ev
}

func samePaths(local *localFacts, remote *remoteFacts, localSHAs map[string]bool) []string {
	var paths []string
	for _, entry := range remote.tree {
		if localSHAs[entry.SHA] && local.blobs[entry.Path] == entry.SHA {
			paths = append(paths, entry.Path)
		}
	}

	sort.Slice(paths, func(i, j int) bool {
		if di, dj := strings.Count(paths[i], "/"), strings.Count(paths[j], "/"); di != dj {
			return di < dj
		}
		return paths[i] < paths[j]
	})
	return paths
}

func rankEvidence(found []evidence) {
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if (a.sharedRoot != nil) != (b.sharedRoot != nil) {
			return a.sharedRoot != nil
		}
		if len(a.shared) != len(b.shared) {
			return len(a.shared) > len(b.shared)
		}
		if a.similarity != b.similarity {
			return a.similarity > b.similarity
		}
		if a.sameName != b.sameName {
			return a.sameName
		}
		return a.remote.item.FullName < b.remote.item.FullName
	})
}

func autoPick(found []evidence) (int, bool) {
	if len(found) == 1 {
		return 0, true
	}
	if found[0].sharedRoot != nil && found[1].sharedRoot == nil {
		return 0, true
	}
	return 0, false
}

type evidenceLine struct {
	color string
	text  string
}

type evidenceLines []evidenceLine

func renderEvidence(ev evidence) []evidenceLine {
	var lines evidenceLines
	if ev.sharedRoot != nil {
		lines.add(render.Green, "✓ same first commit     %s", commitLine(*ev.sharedRoot))
	}

	switch {
	case len(ev.shared) > 0:
		lines.addShared(ev)
	case ev.sharedRoot != nil:
		lines.add(render.Yellow, "✗ none of the latest %d remote commits are in the local history: the histories diverged long ago", len(ev.remote.history.Recent))
		lines.add(render.Grey, "why: an identical first commit SHA means both histories grew from the same start")
	case ev.sameFiles > 0:
		lines.addFiles(ev)
	}
	return lines
}

func (l *evidenceLines) add(color, format string, args ...any) {
	*l = append(*l, evidenceLine{color: color, text: fmt.Sprintf(format, args...)})
}

func (l *evidenceLines) addShared(ev evidence) {
	history := ev.remote.history
	l.add(render.Green, "✓ %s", sharedSummary(len(ev.shared), len(history.Recent), history.Total))
	for _, c := range ev.shared[:min(len(ev.shared), sharedCommitsShown)] {
		l.add("", "    %s", commitLine(c))
	}
	if rest := len(ev.shared) - sharedCommitsShown; rest > 0 {
		l.add(render.Grey, "    …and %d more", rest)
	}

	if text := aheadSummary(ev); text != "" {
		l.add("", "→ %s", text)
	}
	l.add(render.Grey, "why: an identical commit SHA means identical files and identical history up to that commit")
}

func (l *evidenceLines) addFiles(ev evidence) {
	l.add(render.Yellow, "≈ %d%% identical files (%d of %d), no common commits: the history was rewritten or started anew",
		int(ev.similarity*100+0.5), ev.sameFiles, ev.totalFiles)
	if len(ev.samplePaths) > 0 {
		l.add("", "    same content: %s", strings.Join(ev.samplePaths, ", "))
	}
	l.add(render.Grey, "why: files with identical content hashes on the default branch and in the local HEAD")
}

func sharedSummary(shared, recent, total int) string {
	if total > recent {
		return fmt.Sprintf("%d of the latest %d remote commits (of %d) are in the local history", shared, recent, total)
	}
	if shared == recent {
		return fmt.Sprintf("all %d remote commits are in the local history", recent)
	}
	return fmt.Sprintf("%d of %d remote commits are in the local history", shared, recent)
}

func aheadSummary(ev evidence) string {
	if !ev.aheadKnown || ev.remoteAhead < 0 {
		return ""
	}
	switch {
	case ev.localAhead == 0 && ev.remoteAhead == 0:
		return "the local HEAD and the remote default branch point to the same commit"
	case ev.remoteAhead == 0:
		return fmt.Sprintf("local is %s ahead of the remote", plural(ev.localAhead, "commit"))
	case ev.localAhead == 0:
		return fmt.Sprintf("remote is %s ahead of local", plural(ev.remoteAhead, "commit"))
	default:
		return fmt.Sprintf("diverged: local has %s more, remote has %s more", plural(ev.localAhead, "commit"), plural(ev.remoteAhead, "commit"))
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func commitLine(c provider.Commit) string {
	return fmt.Sprintf("%s  %s  %s", shortSHA(c.SHA), commitDate(c), render.Ellipsis(c.Subject, 72))
}

func commitDate(c provider.Commit) string {
	if c.Date.IsZero() {
		return "          "
	}

	return c.Date.Local().Format("2006-01-02")
}

func nameNote(ev evidence) string {
	if ev.sameName {
		return "same name"
	}
	return "name differs"
}

func (s *session) printCandidates(m localMatches) {
	p := s.printer
	p.Line("", "")
	p.Line(render.Bold, "%s  (%s)", shortPath(m.local.item.Path), m.local.item.Reason)
	for i, ev := range m.found {
		p.Line("", "  %d) %s  %s", i+1, p.Colored(render.Bold, ev.remote.item.FullName), p.Colored(render.Grey, "("+nameNote(ev)+")"))
		for _, line := range renderEvidence(ev) {
			p.Line(line.color, "     %s", line.text)
		}
	}
}

func (s *session) linkMatches(all []localMatches, c *confirmer, opts options) fixStats {
	var st fixStats
	used := map[string]bool{}
	for _, m := range all {
		m.found = unusedEvidence(m.found, used)
		if len(m.found) == 0 {
			continue
		}

		s.printCandidates(m)
		if opts.dryRun {
			s.planLink(m, &st)
			continue
		}
		if stop := s.linkOne(m, used, c, opts, &st); stop {
			st.stopped = true
			return st
		}
	}
	return st
}

func unusedEvidence(found []evidence, used map[string]bool) []evidence {
	out := make([]evidence, 0, len(found))
	for _, ev := range found {
		if !used[ev.remote.item.FullName] {
			out = append(out, ev)
		}
	}
	return out
}

func (s *session) planLink(m localMatches, st *fixStats) {
	for _, dead := range m.local.dead {
		s.printer.Line(render.Grey, "  would offer to remove remote %s: %s does not exist on %s", dead.name, dead.fullName, s.prov.Name())
	}

	name := s.plannedRemoteName(m.local)
	s.printer.Line(render.Grey, "  would add remote %s → %s", name, m.found[0].remote.item.FullName)
	st.linkedPairs = append(st.linkedPairs, linkedPair(m.local, m.found[0].remote, name))
}

func (s *session) plannedRemoteName(local *localFacts) string {
	if local.originURL != "" && !local.originDead() {
		return s.prov.Name() + " (asked)"
	}

	return "origin"
}

func linkedPair(local *localFacts, remote *remoteFacts, name string) matched {
	return matched{local: scan.Repo{Path: local.item.Path}, remote: remote.item.repo, remoteName: name}
}

func (s *session) linkOne(m localMatches, used map[string]bool, c *confirmer, opts options, st *fixStats) bool {
	pick, ok, stop := s.pickMatch(m, c, opts, st)
	if stop || !ok {
		return stop
	}

	remote := m.found[pick].remote
	if !s.offerDeadRemoval(m.local, c, opts) {
		s.printer.Line(render.Grey, "Stopped by the user.")
		return true
	}

	name, overwrite, ans, err := s.remoteNameFor(m.local, c)
	if err == nil && ans == answerQuit {
		s.printer.Line(render.Grey, "Stopped by the user.")
		return true
	}
	if err == nil {
		err = s.addLinkRemote(m.local, remote, name, overwrite)
	}
	if err != nil {
		s.printer.Line(render.Red, "  error: %v", err)
		st.failed++
		return false
	}

	used[remote.item.FullName] = true
	st.linked++
	st.linkedPairs = append(st.linkedPairs, linkedPair(m.local, remote, name))
	return false
}

func (s *session) pickMatch(m localMatches, c *confirmer, opts options, st *fixStats) (int, bool, bool) {
	if opts.assumeYes {
		pick, ok := autoPick(m.found)
		if !ok {
			s.printer.Line(render.Yellow, "  skipped: ambiguous, run without --yes to choose")
			st.skipped++
		}
		return pick, ok, false
	}

	idx, ans, err := c.choose(fmt.Sprintf("Link %s with:", shortPath(m.local.item.Path)), len(m.found))
	switch {
	case err != nil:
		s.printer.Line(render.Red, "  %v", err)
		st.failed++
		return 0, false, true
	case ans == answerSkip:
		st.skipped++
		return 0, false, false
	case ans == answerQuit:
		s.printer.Line(render.Grey, "Stopped by the user.")
		return 0, false, true
	default:
		return idx, true, false
	}
}

func (l *localFacts) originDead() bool {
	for _, dead := range l.dead {
		if dead.name == "origin" {
			return true
		}
	}
	return false
}

func (s *session) offerDeadRemoval(local *localFacts, c *confirmer, opts options) bool {
	dir := local.item.Path
	for _, dead := range local.dead {
		if opts.assumeYes {
			s.printer.Line(render.Yellow, "  kept remote %s: %s does not exist on %s, run without --yes to remove it", dead.name, dead.fullName, s.prov.Name())
			continue
		}
		prompt := fmt.Sprintf("  remote %s → %s: %s does not exist on %s", dead.name, dead.url, dead.fullName, s.prov.Name())
		choice, ok, err := c.option(prompt, "remove", "keep")
		if err != nil || !ok {
			return false
		}
		if choice != "remove" {
			continue
		}
		if err := gitcmd.RemoveRemote(s.ctx, dir, dead.name); err != nil {
			s.printer.Line(render.Red, "  error: %v", err)
			continue
		}
		s.printer.Line(render.Grey, "  removed remote %s", dead.name)
	}
	return true
}

func (s *session) remoteNameFor(local *localFacts, c *confirmer) (string, bool, answer, error) {
	dir := local.item.Path
	if !gitcmd.RemoteExists(s.ctx, dir, "origin") {
		return "origin", false, answerYes, nil
	}
	originURL, _ := gitcmd.OriginURL(s.ctx, dir)
	prompt := fmt.Sprintf("  %s already has origin (%s). Name of the new remote", shortPath(dir), originURL)
	for {
		name, ans, err := c.askText(prompt, s.prov.Name(), validateRepoName)
		if err != nil || ans != answerYes {
			return "", false, ans, err
		}
		if !gitcmd.RemoteExists(s.ctx, dir, name) {
			return name, false, answerYes, nil
		}
		url, _ := gitcmd.RemoteURL(s.ctx, dir, name)
		if c.yesToAll {
			return "", false, answerSkip, fmt.Errorf("remote %q already exists (%s), run without --yes to overwrite it", name, url)
		}
		choice, ok, err := c.option(fmt.Sprintf("  remote %s already exists (%s)", name, url), "overwrite", "rename")
		if err != nil || !ok {
			return "", false, answerQuit, err
		}
		if choice == "overwrite" {
			return name, true, answerYes, nil
		}
	}
}

func (s *session) addLinkRemote(local *localFacts, remote *remoteFacts, name string, overwrite bool) error {
	dir := local.item.Path
	url := s.prov.RemoteURL(remote.item.repo, s.linkProtocol(local))
	if err := s.writeLinkRemote(dir, name, url, overwrite); err != nil {
		return err
	}

	branch := remote.item.repo.DefaultBranch
	if err := gitcmd.Fetch(s.ctx, dir, name, branch); err != nil {
		s.printer.Line(render.Yellow, "  warning: %v", err)
		return nil
	}
	if branch != "" {
		if err := gitcmd.SetRemoteHead(s.ctx, dir, name, branch); err != nil {
			s.printer.Line(render.Yellow, "  warning: %v", err)
		}
	}
	return nil
}

func (s *session) linkProtocol(local *localFacts) string {
	if s.opts.protocol == "" && local.originURL != "" {
		return protocolOf(local.originURL)
	}

	return s.cfg.Protocol
}

func (s *session) writeLinkRemote(dir, name, url string, overwrite bool) error {
	if !overwrite {
		if err := gitcmd.AddRemote(s.ctx, dir, name, url); err != nil {
			return err
		}
		s.printer.Line(render.Green, "  linked: remote %s → %s", name, url)
		return nil
	}

	if err := gitcmd.SetRemote(s.ctx, dir, name, url); err != nil {
		return err
	}
	s.printer.Line(render.Green, "  linked: remote %s overwritten → %s", name, url)
	return nil
}

func differentNames(pairs []matched) []matched {
	var out []matched
	for _, pair := range pairs {
		if filepath.Base(pair.local.Path) != pair.remote.Name {
			out = append(out, pair)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].local.Path < out[j].local.Path })
	return out
}

func (s *session) syncNames(pairs []matched, account []provider.Repo, c *confirmer, opts options, st *fixStats) {
	for _, pair := range pairs {
		if filepath.Base(pair.local.Path) == pair.remote.Name {
			continue
		}

		st.namesDiffer++
		if row, blocked := s.guard.check(pair.local.Path); blocked {
			st.blocked = append(st.blocked, row)
			st.skipped++
			continue
		}

		s.printNamesDiffer(pair)
		switch {
		case opts.dryRun:
			s.printer.Line(render.Grey, "  would ask which one to rename")
		case opts.assumeYes:
			s.printer.Line(render.Yellow, "  kept: run without --yes to choose which one to rename")
			st.skipped++
		case s.chooseRename(pair, account, c, st):
			st.stopped = true
			return
		}
	}
}

func (s *session) printNamesDiffer(pair matched) {
	dir := pair.local.Path
	target := filepath.Join(filepath.Dir(dir), pair.remote.Name)
	renamedFull := pair.remote.Owner + "/" + filepath.Base(dir)

	p := s.printer
	p.Line("", "")
	p.Line(render.Bold, "Names differ: %s  ↔  %s", shortPath(dir), pair.remote.FullName())
	p.Line("", "  local   %-22s %s → %s", "rename the directory", shortPath(dir), shortPath(target))
	p.Line("", "  remote  %-22s %s → %s", "rename on "+s.prov.Name(), pair.remote.FullName(), renamedFull)
	p.Line("", "  keep    leave both names as they are")
}

func (s *session) chooseRename(pair matched, account []provider.Repo, c *confirmer, st *fixStats) bool {
	for {
		choice, ok, err := c.option("  Rename which one?", "local", "remote", "keep")
		if err != nil || !ok {
			s.reportStop(err, st)
			return true
		}
		if choice == "keep" {
			st.skipped++
			return false
		}

		err = s.renameSide(pair, choice, account)
		switch {
		case err == nil:
			st.renamed++
			return false
		case errors.Is(err, errNameConflict):
			s.printer.Line(render.Red, "  error: %v", err)
		default:
			s.printer.Line(render.Red, "  error: %v", err)
			st.failed++
			return false
		}
	}
}

func (s *session) reportStop(err error, st *fixStats) {
	if err != nil {
		s.printer.Line(render.Red, "  %v", err)
		st.failed++
		return
	}

	s.printer.Line(render.Grey, "Stopped by the user.")
}

func (s *session) renameSide(pair matched, choice string, account []provider.Repo) error {
	dir := pair.local.Path
	if choice == "local" {
		return s.renameLocalDir(dir, filepath.Join(filepath.Dir(dir), pair.remote.Name))
	}

	return s.renameRemoteTo(pair, filepath.Base(dir), account)
}

var errNameConflict = errors.New("name conflict")

func (s *session) renameLocalDir(dir, target string) error {
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%w: %s already exists, pick another option", errNameConflict, shortPath(target))
	}
	if err := os.Rename(dir, target); err != nil {
		return err
	}
	s.printer.Line(render.Green, "  renamed: %s → %s", shortPath(dir), shortPath(target))
	return nil
}

func (s *session) renameRemoteTo(pair matched, newName string, account []provider.Repo) error {
	if err := validateRepoName(newName); err != nil {
		return fmt.Errorf("%w: %v, pick another option", errNameConflict, err)
	}
	for _, repo := range account {
		if strings.EqualFold(repo.Owner, pair.remote.Owner) && strings.EqualFold(repo.Name, newName) {
			return fmt.Errorf("%w: %s already exists on %s, pick another option", errNameConflict, repo.FullName(), s.prov.Name())
		}
	}
	dir := pair.local.Path
	currentURL, _ := gitcmd.RemoteURL(s.ctx, dir, pair.gitRemote())
	protocol := remoteProtocol(currentURL, s.cfg.Protocol)
	renamed, err := s.prov.RenameRepo(s.ctx, pair.remote.Owner, pair.remote.Name, newName)
	if err != nil {
		if errors.Is(err, provider.ErrExists) {
			return fmt.Errorf("%w: %v, pick another option", errNameConflict, err)
		}
		return err
	}
	if err := gitcmd.SetRemote(s.ctx, dir, pair.gitRemote(), s.prov.RemoteURL(renamed, protocol)); err != nil {
		return fmt.Errorf("%s renamed to %s, but %s was not updated: %w", pair.remote.FullName(), renamed.FullName(), pair.gitRemote(), err)
	}
	s.printer.Line(render.Green, "  renamed: %s → %s, remote %s updated", pair.remote.FullName(), renamed.FullName(), pair.gitRemote())
	return nil
}
