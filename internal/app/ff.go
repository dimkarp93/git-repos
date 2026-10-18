package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sort"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/render"
)

type ffOutcome string

const (
	ffForwarded  ffOutcome = "forwarded"
	ffPushed     ffOutcome = "pushed"
	ffUpToDate   ffOutcome = "up-to-date"
	ffAhead      ffOutcome = "ahead"
	ffBehind     ffOutcome = "behind"
	ffNotFetched ffOutcome = "not-fetched"
	ffConflict   ffOutcome = "conflict"
	ffSkipped    ffOutcome = "skipped"
	ffError      ffOutcome = "error"
)

type ffResult struct {
	FullName    string
	Path        string
	dir         string
	Remote      string
	Branch      string
	Outcome     ffOutcome
	LocalSHA    string
	RemoteSHA   string
	Ahead       int
	Behind      int
	CheckedOut  bool
	UpstreamSet bool
	Detail      string
}

func (r ffResult) remote() string {
	if r.Remote == "" {
		return "origin"
	}
	return r.Remote
}

type branchAction func(ctx context.Context, dir, remote, branch string) ffResult

func ffFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("ff", opts)
	fs.BoolVar(&opts.refresh, "refresh", false, "ignore cached default branches")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	return fs
}

func runFF(args []string) int {
	return runBranchAction("ff", ffFlags, fastForward, printFF, args)
}

func runBranchAction(phase string, flags func(*options) *flag.FlagSet, action branchAction, print func(*render.Printer, []ffResult) int, args []string) int {
	opts := options{}
	fs := flags(&opts)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(4)
	s.spinner.Start(s.pr.label)
	pairs, _, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		return fail(err)
	}
	results := forEachDefault(s.ctx, s, pairs, opts, s.pr, phase, action)
	s.spinner.Stop()

	return print(s.printer, results)
}

func printFF(p *render.Printer, results []ffResult) int {
	counts := printBranchTable(p, "Fast-forward result", results)
	p.Line("", "Summary: %s · up to date — %d · %s · %s · skipped — %d · %s",
		p.Colored(render.Green, fmt.Sprintf("fast-forwarded — %d", counts[ffForwarded])),
		counts[ffUpToDate],
		p.Colored(render.Orange, fmt.Sprintf("ahead — %d", counts[ffAhead])),
		p.Colored(render.Red, fmt.Sprintf("conflicts — %d", counts[ffConflict])),
		counts[ffSkipped],
		p.Colored(render.Red, fmt.Sprintf("errors — %d", counts[ffError])),
	)
	return finishBranchResults(p, results, counts)
}

func printBranchTable(p *render.Printer, title string, results []ffResult) map[ffOutcome]int {
	rows := make([][]render.Cell, 0, len(results))
	counts := map[ffOutcome]int{}
	for _, res := range results {
		counts[res.Outcome]++
		rows = append(rows, []render.Cell{
			{Text: res.FullName, Color: render.Bold},
			render.Plain(res.Branch),
			ffCell(res),
			{Text: render.Ellipsis(res.Path, maxPathWidth), Color: render.Grey},
		})
	}
	p.Table(title, []string{"REPOSITORY", "BRANCH", "RESULT", "PATH"}, rows)
	return counts
}

func finishBranchResults(p *render.Printer, results []ffResult, counts map[ffOutcome]int) int {
	for _, res := range results {
		if res.Outcome == ffConflict {
			printConflict(p, res)
		}
	}
	if counts[ffConflict] > 0 || counts[ffError] > 0 {
		return ExitFailure
	}
	return ExitOK
}

func ffCell(res ffResult) render.Cell {
	switch res.Outcome {
	case ffForwarded:
		return render.Cell{Text: fmt.Sprintf("fast-forwarded +%d", res.Behind), Color: render.Green}
	case ffPushed:
		return render.Cell{Text: pushedText(res), Color: render.Green}
	case ffUpToDate:
		return render.Cell{Text: "up to date", Color: render.Grey}
	case ffAhead:
		return render.Cell{Text: fmt.Sprintf("ahead +%d → push", res.Ahead), Color: render.Orange}
	case ffBehind:
		return render.Cell{Text: fmt.Sprintf("behind +%d → ff", res.Behind), Color: render.Orange}
	case ffConflict:
		return render.Cell{Text: fmt.Sprintf("conflict: +%d local, +%d %s", res.Ahead, res.Behind, res.remote()), Color: render.Red}
	case ffSkipped:
		return render.Cell{Text: render.Ellipsis("skipped: "+res.Detail, maxDetailWidth), Color: render.Grey}
	default:
		return render.Cell{Text: render.Ellipsis("error: "+res.Detail, maxDetailWidth), Color: render.Red}
	}
}

func pushedText(res ffResult) string {
	var text string
	if res.RemoteSHA == "" {
		text = "pushed"
	} else {
		text = fmt.Sprintf("pushed +%d", res.Ahead)
	}

	if res.UpstreamSet {
		return text + ", upstream set"
	}
	return text
}

func printConflict(p *render.Printer, res ffResult) {
	b := res.Branch
	r := res.remote()

	p.Line("", "")
	p.Line(render.Red, "Version conflict: %s (%s) — local %s and %s have diverged", res.FullName, b, b, r+"/"+b)
	p.Line("", "  local  %s  +%d commits not on %s", shortSHA(res.LocalSHA), res.Ahead, r)
	p.Line("", "  %-6s %s  +%d commits not in local", r, shortSHA(res.RemoteSHA), res.Behind)
	p.Line("", "")

	printConflictOptions(p, res)
	p.Line(render.Grey, "  These commands are only suggestions, git-repos does not run them.")
}

func printConflictOptions(p *render.Printer, res ffResult) {
	g := "git -C " + shellQuote(res.dir) + " "
	b := res.Branch
	r := res.remote()
	origin := r + "/" + b
	code := func(cmd string) { p.Line(render.Blue, "       %s", cmd) }

	p.Line(render.Bold, "  1. Merge the histories, resolve conflicts and commit, then push:")
	if !res.CheckedOut {
		code(g + "checkout " + b)
	}
	code(g + "merge " + origin)
	code(g + "push " + r + " " + b)

	p.Line(render.Bold, "  2. Keep local, overwrite the history on %s:", r)
	code(g + "push --force-with-lease=" + b + ":" + res.RemoteSHA + " " + r + " " + b)

	p.Line(render.Bold, "  3. Keep %s, overwrite the local history (local commits stay in the backup branch):", r)
	code(g + "branch backup/" + b + "-" + shortSHA(res.LocalSHA) + " " + b)
	code(g + resetCommand(res.CheckedOut, b, origin))
}

func resetCommand(checkedOut bool, branch, origin string) string {
	if checkedOut {
		return "reset --hard " + origin
	}

	return "branch -f " + branch + " " + origin
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func forEachDefault(ctx context.Context, s *session, pairs []matched, opts options, pr *progress, phase string, action branchAction) []ffResult {
	results := make([]ffResult, len(pairs))
	pr.setPhase(phase)
	pr.setTotal(len(pairs))

	parallel(opts.jobs, len(pairs), func(i int) {
		pr.begin(pairs[i].remote.FullName())
		defer pr.end()
		results[i] = runDefault(ctx, s, pairs[i], opts, action)
	})

	sort.Slice(results, func(i, j int) bool { return results[i].FullName < results[j].FullName })
	return results
}

func runDefault(ctx context.Context, s *session, pair matched, opts options, action branchAction) ffResult {
	branch := resolveBranch(s.store, s.prov.Name(), pair.remote, opts.refresh)
	res := action(ctx, pair.local.Path, pair.gitRemote(), branch)
	res.FullName = pair.label()
	res.Path = shortPath(pair.local.Path)
	return res
}

func compareDefault(ctx context.Context, dir, remoteName, branch string) ffResult {
	res := ffResult{dir: dir, Remote: remoteName, Branch: branch}
	if branch == "" {
		return res.fail(errDefaultBranchUnknown)
	}

	local, err := gitcmd.RevParse(ctx, dir, "refs/heads/"+branch)
	if errors.Is(err, gitcmd.ErrNoRef) {
		res.Outcome = ffSkipped
		res.Detail = "no local " + branch
		return res
	}
	if err != nil {
		return res.fail(err)
	}
	res.LocalSHA = local
	current, _ := gitcmd.CurrentBranch(ctx, dir)
	res.CheckedOut = current == branch

	remote, err := gitcmd.RevParse(ctx, dir, "refs/remotes/"+remoteName+"/"+branch)
	if errors.Is(err, gitcmd.ErrNoRef) {
		res.Outcome = ffNotFetched
		return res
	}
	if err != nil {
		return res.fail(err)
	}
	res.RemoteSHA = remote
	return res.diverged(ctx)
}

func (r ffResult) diverged(ctx context.Context) ffResult {
	if r.LocalSHA == r.RemoteSHA {
		r.Outcome = ffUpToDate
		return r
	}

	var err error
	if r.Ahead, err = gitcmd.CountCommits(ctx, r.dir, r.RemoteSHA, r.LocalSHA); err != nil {
		return r.fail(err)
	}
	if r.Behind, err = gitcmd.CountCommits(ctx, r.dir, r.LocalSHA, r.RemoteSHA); err != nil {
		return r.fail(err)
	}

	r.Outcome = divergence(r.Ahead, r.Behind)
	return r
}

func divergence(ahead, behind int) ffOutcome {
	switch {
	case ahead > 0 && behind > 0:
		return ffConflict
	case ahead > 0:
		return ffAhead
	default:
		return ffBehind
	}
}

func fastForward(ctx context.Context, dir, remoteName, branch string) ffResult {
	res := compareDefault(ctx, dir, remoteName, branch)
	switch res.Outcome {
	case ffNotFetched:
		res.Outcome = ffError
		res.Detail = remoteName + "/" + branch + " not fetched, run update"
		return res
	case ffBehind:
	default:
		return res
	}

	if err := forwardBranch(ctx, res); err != nil {
		return res.fail(err)
	}
	res.Outcome = ffForwarded
	return res
}

func forwardBranch(ctx context.Context, res ffResult) error {
	if res.CheckedOut {
		return gitcmd.MergeFFOnly(ctx, res.dir, "refs/remotes/"+res.Remote+"/"+res.Branch)
	}

	return gitcmd.FastForwardBranch(ctx, res.dir, res.Remote, res.Branch)
}

func (r ffResult) fail(err error) ffResult {
	r.Outcome = ffError
	r.Detail = errText(err)
	return r
}
