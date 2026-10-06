package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
	if opts.protocol != "" && opts.protocol != provider.ProtocolSSH && opts.protocol != provider.ProtocolHTTPS {
		return fail(errors.New("--protocol accepts ssh or https"))
	}
	if opts.minSimilarity <= 0 || opts.minSimilarity > 1 {
		return fail(errors.New("--min-similarity accepts a value above 0 and up to 1"))
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
	var candidates []localMatches
	if len(inv.localOnly) > 0 && len(inv.remoteOnly) > 0 {
		locals := s.localFacts(inv.localOnly, inv.remotes, opts)
		remotes := s.remoteFacts(inv.remoteOnly, opts)
		candidates = s.findMatches(locals, remotes, opts)
	}
	s.spinner.Stop()

	differ := differentNames(inv.pairs)
	if len(candidates) == 0 && len(differ) == 0 {
		s.printer.Line(render.Grey, "Nothing to fix: no local-only repository shares history or files with a remote-only one, and linked repositories have the same names.")
		return ExitOK
	}

	var c *confirmer
	if !opts.dryRun {
		c = newConfirmer(opts.assumeYes)
	}
	stats := s.linkMatches(candidates, c, opts)
	if !stats.stopped {
		s.syncNames(append(stats.linkedPairs, differ...), inv.remotes, c, opts, &stats)
	}
	if opts.dryRun {
		s.printer.Line("", "Summary: possible links — %d · names differ — %d", len(candidates), stats.namesDiffer)
		return ExitOK
	}
	s.printer.Line("", "Summary: linked — %d · renamed — %d · skipped — %d · errors — %d", stats.linked, stats.renamed, stats.skipped, stats.failed)
	if stats.failed > 0 {
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
}

func (s *session) localFacts(items []LocalOnly, account []provider.Repo, opts options) []*localFacts {
	existing := make(map[string]bool, len(account))
	for _, repo := range account {
		existing[strings.ToLower(repo.FullName())] = true
	}
	usable := make([]LocalOnly, 0, len(items))
	for _, item := range items {
		if item.Kind != KindUnreadable && gitcmd.HasCommits(s.ctx, item.Path) {
			usable = append(usable, item)
		}
	}
	out := make([]*localFacts, len(usable))
	s.pr.setPhase("reading local history")
	s.pr.setUnit("repositories")
	s.pr.setTotal(len(usable))
	parallel(opts.jobs, len(usable), func(i int) {
		item := usable[i]
		s.pr.begin(shortPath(item.Path))
		defer s.pr.end()
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
		out[i] = facts
	})
	return out
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
		item := items[i]
		s.pr.begin(item.FullName)
		defer s.pr.end()
		history, err := s.prov.History(s.ctx, item.repo.Owner, item.repo.Name, item.repo.DefaultBranch)
		if err != nil {
			return
		}
		all[i] = &remoteFacts{item: item, history: history}
	})
	out := make([]*remoteFacts, 0, len(all))
	for _, facts := range all {
		if facts != nil {
			out = append(out, facts)
		}
	}
	return out
}

func parallel(jobs, n int, fn func(i int)) {
	sem := make(chan struct{}, max(jobs, 1))
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn(i)
		}()
	}
	wg.Wait()
}

type localMatches struct {
	local *localFacts
	found []evidence
}

func (s *session) findMatches(locals []*localFacts, remotes []*remoteFacts, opts options) []localMatches {
	byHistory := make([][]evidence, len(locals))
	needFiles := false
	for i, local := range locals {
		for _, remote := range remotes {
			if ev := scoreMatch(local, remote); ev.byHistory() {
				byHistory[i] = append(byHistory[i], ev)
			}
		}
		if len(byHistory[i]) == 0 {
			needFiles = true
		}
	}
	if needFiles {
		s.loadTrees(remotes, opts)
	}

	var out []localMatches
	for i, local := range locals {
		found := byHistory[i]
		if len(found) == 0 {
			for _, remote := range remotes {
				if ev := scoreMatch(local, remote); ev.matches(opts.minSimilarity) {
					found = append(found, ev)
				}
			}
		}
		if len(found) == 0 {
			continue
		}
		for j := range found {
			s.measureAhead(local, &found[j])
		}
		rankEvidence(found)
		out = append(out, localMatches{local: local, found: found})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].local.item.Path < out[j].local.item.Path })
	return out
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
	if ev.byHistory() || len(remote.tree) == 0 || len(local.blobs) == 0 {
		return ev
	}

	localSHAs := map[string]bool{}
	for _, sha := range local.blobs {
		localSHAs[sha] = true
	}
	remoteSHAs := map[string]bool{}
	var paths []string
	for _, entry := range remote.tree {
		remoteSHAs[entry.SHA] = true
		if localSHAs[entry.SHA] && local.blobs[entry.Path] == entry.SHA {
			paths = append(paths, entry.Path)
		}
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
	sort.Slice(paths, func(i, j int) bool {
		if di, dj := strings.Count(paths[i], "/"), strings.Count(paths[j], "/"); di != dj {
			return di < dj
		}
		return paths[i] < paths[j]
	})
	ev.sameFiles = common
	ev.totalFiles = union
	ev.similarity = float64(common) / float64(union)
	ev.samplePaths = paths[:min(len(paths), samePathsShown)]
	return ev
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

func renderEvidence(ev evidence) []evidenceLine {
	var lines []evidenceLine
	add := func(color, format string, args ...any) {
		lines = append(lines, evidenceLine{color: color, text: fmt.Sprintf(format, args...)})
	}
	history := ev.remote.history
	if ev.sharedRoot != nil {
		add(render.Green, "✓ same first commit     %s", commitLine(*ev.sharedRoot))
	}
	switch {
	case len(ev.shared) > 0:
		add(render.Green, "✓ %s", sharedSummary(len(ev.shared), len(history.Recent), history.Total))
		for _, c := range ev.shared[:min(len(ev.shared), sharedCommitsShown)] {
			add("", "    %s", commitLine(c))
		}
		if rest := len(ev.shared) - sharedCommitsShown; rest > 0 {
			add(render.Grey, "    …and %d more", rest)
		}
		if text := aheadSummary(ev); text != "" {
			add("", "→ %s", text)
		}
		add(render.Grey, "why: an identical commit SHA means identical files and identical history up to that commit")
	case ev.sharedRoot != nil:
		add(render.Yellow, "✗ none of the latest %d remote commits are in the local history: the histories diverged long ago", len(history.Recent))
		add(render.Grey, "why: an identical first commit SHA means both histories grew from the same start")
	case ev.sameFiles > 0:
		add(render.Yellow, "≈ %d%% identical files (%d of %d), no common commits: the history was rewritten or started anew",
			int(ev.similarity*100+0.5), ev.sameFiles, ev.totalFiles)
		if len(ev.samplePaths) > 0 {
			add("", "    same content: %s", strings.Join(ev.samplePaths, ", "))
		}
		add(render.Grey, "why: files with identical content hashes on the default branch and in the local HEAD")
	}
	return lines
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
	date := "          "
	if !c.Date.IsZero() {
		date = c.Date.Local().Format("2006-01-02")
	}
	return fmt.Sprintf("%s  %s  %s", shortSHA(c.SHA), date, render.Ellipsis(c.Subject, 72))
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
		found := make([]evidence, 0, len(m.found))
		for _, ev := range m.found {
			if !used[ev.remote.item.FullName] {
				found = append(found, ev)
			}
		}
		if len(found) == 0 {
			continue
		}
		m.found = found
		s.printCandidates(m)

		if opts.dryRun {
			for _, dead := range m.local.dead {
				s.printer.Line(render.Grey, "  would offer to remove remote %s: %s does not exist on %s", dead.name, dead.fullName, s.prov.Name())
			}
			name := "origin"
			if m.local.originURL != "" && !m.local.originDead() {
				name = s.prov.Name() + " (asked)"
			}
			s.printer.Line(render.Grey, "  would add remote %s → %s", name, found[0].remote.item.FullName)
			st.linkedPairs = append(st.linkedPairs, matched{local: scan.Repo{Path: m.local.item.Path}, remote: found[0].remote.item.repo, remoteName: name})
			continue
		}

		pick, ok := 0, true
		if opts.assumeYes {
			if pick, ok = autoPick(found); !ok {
				s.printer.Line(render.Yellow, "  skipped: ambiguous, run without --yes to choose")
				st.skipped++
				continue
			}
		} else {
			idx, ans, err := c.choose(fmt.Sprintf("Link %s with:", shortPath(m.local.item.Path)), len(found))
			if err != nil {
				s.printer.Line(render.Red, "  %v", err)
				st.failed++
				st.stopped = true
				return st
			}
			switch ans {
			case answerSkip:
				st.skipped++
				continue
			case answerQuit:
				s.printer.Line(render.Grey, "Stopped by the user.")
				st.stopped = true
				return st
			}
			pick = idx
		}

		remote := found[pick].remote
		if !s.offerDeadRemoval(m.local, c, opts) {
			s.printer.Line(render.Grey, "Stopped by the user.")
			st.stopped = true
			return st
		}
		name, overwrite, ans, err := s.remoteNameFor(m.local, c)
		if err != nil {
			s.printer.Line(render.Red, "  error: %v", err)
			st.failed++
			continue
		}
		if ans == answerQuit {
			s.printer.Line(render.Grey, "Stopped by the user.")
			st.stopped = true
			return st
		}
		if err := s.addLinkRemote(m.local, remote, name, overwrite); err != nil {
			s.printer.Line(render.Red, "  error: %v", err)
			st.failed++
			continue
		}
		used[remote.item.FullName] = true
		st.linked++
		st.linkedPairs = append(st.linkedPairs, matched{local: scan.Repo{Path: m.local.item.Path}, remote: remote.item.repo, remoteName: name})
	}
	return st
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
	protocol := s.cfg.Protocol
	if s.opts.protocol == "" && local.originURL != "" {
		protocol = protocolOf(local.originURL)
	}
	url := s.prov.RemoteURL(remote.item.repo, protocol)
	if overwrite {
		if err := gitcmd.SetRemote(s.ctx, dir, name, url); err != nil {
			return err
		}
		s.printer.Line(render.Green, "  linked: remote %s overwritten → %s", name, url)
	} else {
		if err := gitcmd.AddRemote(s.ctx, dir, name, url); err != nil {
			return err
		}
		s.printer.Line(render.Green, "  linked: remote %s → %s", name, url)
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
		localName := filepath.Base(pair.local.Path)
		if localName == pair.remote.Name {
			continue
		}
		st.namesDiffer++
		dir := pair.local.Path
		target := filepath.Join(filepath.Dir(dir), pair.remote.Name)
		renamedFull := pair.remote.Owner + "/" + localName
		p := s.printer
		p.Line("", "")
		p.Line(render.Bold, "Names differ: %s  ↔  %s", shortPath(dir), pair.remote.FullName())
		p.Line("", "  local   %-22s %s → %s", "rename the directory", shortPath(dir), shortPath(target))
		p.Line("", "  remote  %-22s %s → %s", "rename on "+s.prov.Name(), pair.remote.FullName(), renamedFull)
		p.Line("", "  keep    leave both names as they are")
		if opts.dryRun {
			p.Line(render.Grey, "  would ask which one to rename")
			continue
		}
		if opts.assumeYes {
			p.Line(render.Yellow, "  kept: run without --yes to choose which one to rename")
			st.skipped++
			continue
		}
		for {
			choice, ok, err := c.option("  Rename which one?", "local", "remote", "keep")
			if err != nil || !ok {
				if err != nil {
					p.Line(render.Red, "  %v", err)
					st.failed++
				} else {
					p.Line(render.Grey, "Stopped by the user.")
				}
				st.stopped = true
				return
			}
			if choice == "keep" {
				st.skipped++
				break
			}
			var renameErr error
			if choice == "local" {
				renameErr = s.renameLocalDir(dir, target)
			} else {
				renameErr = s.renameRemoteTo(pair, localName, account)
			}
			if renameErr == nil {
				st.renamed++
				break
			}
			p.Line(render.Red, "  error: %v", renameErr)
			if errors.Is(renameErr, errNameConflict) {
				continue
			}
			st.failed++
			break
		}
	}
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
	protocol := s.cfg.Protocol
	if currentURL != "" {
		protocol = protocolOf(currentURL)
	}
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
