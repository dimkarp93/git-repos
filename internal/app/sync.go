package app

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"

	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
)

type syncAction struct {
	Target string
	Detail string
	URL    string
	Err    error
	Done   bool
	Plan   bool
}

func syncFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("sync", opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "show what would be done and change nothing")
	fs.StringVar(&opts.into, "into", "", "directory for cloned repositories (required)")
	fs.StringVar(&opts.protocol, "protocol", "", "remote protocol for new remotes and clones: ssh or https")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	return fs
}

func runSync(args []string) int {
	opts := options{}
	fs := syncFlags(&opts)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	into, err := syncTarget(opts)
	if err != nil {
		return fail(err)
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(6)
	s.spinner.Start(s.pr.label)
	pairs, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		return fail(err)
	}

	pushed := s.pushMissing(inv, opts)
	filled := s.fillEmpty(pairs, opts)
	cloned := s.cloneMissing(inv, into, opts)
	s.spinner.Stop()

	report(s, "CREATED ON "+upperName(s.prov.Name()), pushed)
	report(s, "PUSHED INTO EMPTY ON "+upperName(s.prov.Name()), filled)
	report(s, "CLONED INTO "+render.Ellipsis(shortPath(into), maxPathWidth), cloned)
	printLinks(s.printer, slices.Concat(pushed, filled))

	return syncSummary(s.printer, pushed, filled, cloned)
}

func syncTarget(opts options) (string, error) {
	if opts.protocol != "" && opts.protocol != provider.ProtocolSSH && opts.protocol != provider.ProtocolHTTPS {
		return "", errors.New("--protocol accepts ssh or https")
	}
	if opts.into == "" {
		return "", errors.New("--into <dir> is required: the directory to clone remote repositories into")
	}

	into := config.ExpandPath(opts.into)
	if info, err := os.Stat(into); err != nil || !info.IsDir() {
		return "", errors.New("clone directory is not accessible: " + into)
	}
	return into, nil
}

func printLinks(p *render.Printer, actions []syncAction) {
	var links []string
	for _, action := range actions {
		if action.Done && action.URL != "" {
			links = append(links, action.URL)
		}
	}
	if len(links) == 0 {
		return
	}

	p.Line(render.Bold, "Links to the created repositories:")
	for _, link := range links {
		p.Line("", "  %s", link)
	}
	p.Line("", "")
}

func syncSummary(p *render.Printer, pushed, filled, cloned []syncAction) int {
	failed := countFailed(pushed) + countFailed(filled) + countFailed(cloned)
	p.Line("", "Summary: created — %d · pushed into empty — %d · cloned — %d · skipped with an error — %d",
		countDone(pushed), countDone(filled), countDone(cloned), failed)

	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func (s *session) pushMissing(inv inventory, opts options) []syncAction {
	actions := make([]syncAction, 0, len(inv.localOnly))
	s.pr.setPhase("creating on " + s.prov.Name())
	s.pr.setUnit("repositories")
	s.pr.setTotal(countOrphans(inv))

	for _, item := range inv.localOnly {
		if item.Orphan() {
			s.pr.step(item.Name, len(actions))
			actions = append(actions, s.pushOne(inv, item, opts))
		}
	}
	s.pr.complete()
	return actions
}

func (s *session) pushOne(inv inventory, item LocalOnly, opts options) syncAction {
	action := syncAction{Target: shortPath(item.Path), Detail: item.Name}
	switch {
	case item.Owner != "" && item.Owner != inv.account:
		action.Detail = item.Owner + "/" + item.Name + ": another owner"
		return action
	case nameTaken(inv, inv.account, item.Name):
		action.Detail = item.Name + ": name taken, conflict"
		return action
	case opts.dryRun:
		action.Detail = "would create a private " + inv.account + "/" + item.Name
		action.Plan = true
		return action
	}

	created, err := s.prov.CreateRepo(s.ctx, item.Name, true)
	if err != nil {
		action.Err = err
		return action
	}
	action.Done = true
	action.URL = created.WebURL
	action.Detail = created.FullName()
	return s.publish(item.Path, created, action)
}

func (s *session) publish(dir string, created provider.Repo, action syncAction) syncAction {
	if err := gitcmd.SetRemote(s.ctx, dir, "origin", s.prov.RemoteURL(created, s.cfg.Protocol)); err != nil {
		action.Err = err
		return action
	}
	if !gitcmd.HasCommits(s.ctx, dir) {
		action.Detail += " (empty, push skipped)"
		return action
	}

	branch, err := gitcmd.CurrentBranch(s.ctx, dir)
	if err == nil {
		err = gitcmd.Push(s.ctx, dir, "origin", branch)
	}
	if err != nil {
		action.Err = err
		return action
	}
	action.Detail += " ← " + branch
	return action
}

func (s *session) fillEmpty(pairs []matched, opts options) []syncAction {
	s.pr.setPhase("looking for empty ones on " + s.prov.Name())
	s.pr.setUnit("repositories")
	s.pr.setTotal(len(pairs))

	actions := make([]syncAction, 0)
	for i, pair := range pairs {
		s.pr.step(pair.remote.FullName(), i)
		if s.remoteEmpty(pair, opts) {
			actions = append(actions, s.fillOne(pair, opts))
		}
	}
	s.pr.complete()
	return actions
}

func (s *session) remoteEmpty(pair matched, opts options) bool {
	if !gitcmd.HasCommits(s.ctx, pair.local.Path) || gitcmd.HasRemoteRefs(s.ctx, pair.local.Path, pair.gitRemote()) {
		return false
	}

	branch := resolveBranch(s.store, s.prov.Name(), pair.remote, opts.refresh)
	if branch == "" {
		return false
	}
	_, err := s.prov.BranchHead(s.ctx, pair.remote.Owner, pair.remote.Name, branch)
	return errors.Is(err, provider.ErrNotFound)
}

func (s *session) fillOne(pair matched, opts options) syncAction {
	action := syncAction{Target: shortPath(pair.local.Path), Detail: pair.remote.FullName() + ": the remote is empty", URL: pair.remote.WebURL}
	if opts.dryRun {
		action.Detail = "would push into the empty " + pair.remote.FullName()
		action.Plan = true
		return action
	}

	local, err := gitcmd.CurrentBranch(s.ctx, pair.local.Path)
	if err == nil {
		err = gitcmd.Push(s.ctx, pair.local.Path, pair.gitRemote(), local)
	}
	if err != nil {
		action.Err = err
		return action
	}
	action.Done = true
	action.Detail = pair.remote.FullName() + " ← " + local
	return action
}

func (s *session) cloneMissing(inv inventory, into string, opts options) []syncAction {
	actions := make([]syncAction, 0, len(inv.remoteOnly))
	s.pr.setPhase("cloning into " + shortPath(into))
	s.pr.setUnit("repositories")
	s.pr.setTotal(len(inv.remoteOnly))

	for _, item := range inv.remoteOnly {
		s.pr.step(item.FullName, len(actions))
		actions = append(actions, s.cloneOne(item, into, opts))
	}
	s.pr.complete()
	return actions
}

func (s *session) cloneOne(item RemoteOnly, into string, opts options) syncAction {
	dest := filepath.Join(into, item.repo.Name)
	action := syncAction{Target: item.FullName, Detail: shortPath(dest), URL: item.WebURL}
	if _, err := os.Stat(dest); err == nil {
		action.Detail = shortPath(dest) + ": directory taken, conflict"
		return action
	}
	if opts.dryRun {
		action.Detail = "would clone into " + shortPath(dest)
		action.Plan = true
		return action
	}

	if err := gitcmd.Clone(s.ctx, into, s.prov.RemoteURL(item.repo, s.cfg.Protocol), item.repo.Name, item.repo.DefaultBranch); err != nil {
		action.Err = err
		return action
	}
	action.Done = true
	return action
}

func countOrphans(inv inventory) int {
	n := 0
	for _, item := range inv.localOnly {
		if item.Orphan() {
			n++
		}
	}
	return n
}

func nameTaken(inv inventory, account, name string) bool {
	for _, repo := range inv.remotes {
		if repo.Owner == account && repo.Name == name {
			return true
		}
	}
	return false
}

func report(s *session, title string, actions []syncAction) {
	if len(actions) == 0 {
		return
	}

	rows := make([][]render.Cell, 0, len(actions))
	for _, action := range actions {
		rows = append(rows, []render.Cell{
			render.Plain(render.Ellipsis(action.Target, maxPathWidth)),
			{Text: render.Ellipsis(action.Detail, maxDetailWidth), Color: render.Grey},
			syncStatus(action),
		})
	}
	s.printer.Table(title, []string{"SOURCE", "TARGET", "STATUS"}, rows)
	s.printer.Line("", "")
}

func syncStatus(action syncAction) render.Cell {
	switch {
	case action.Err != nil:
		return render.Cell{Text: "error: " + errText(action.Err), Color: render.Red}
	case action.Done:
		return render.Cell{Text: "done", Color: render.Green}
	case action.Plan:
		return render.Cell{Text: "plan", Color: render.Grey}
	default:
		return render.Cell{Text: "skipped", Color: render.Yellow}
	}
}

func countDone(actions []syncAction) int {
	n := 0
	for _, action := range actions {
		if action.Done {
			n++
		}
	}
	return n
}

func countFailed(actions []syncAction) int {
	n := 0
	for _, action := range actions {
		if action.Err != nil {
			n++
		}
	}
	return n
}

func upperName(s string) string {
	switch s {
	case "github":
		return "GITHUB"
	default:
		return s
	}
}
