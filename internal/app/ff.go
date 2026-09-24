package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sort"
	"sync"

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

type branchAction func(ctx context.Context, dir, branch string) ffResult

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
		text := fmt.Sprintf("pushed +%d", res.Ahead)
		if res.RemoteSHA == "" {
			text = "pushed"
		}
		if res.UpstreamSet {
			text += ", upstream set"
		}
		return render.Cell{Text: text, Color: render.Green}
	case ffUpToDate:
		return render.Cell{Text: "up to date", Color: render.Grey}
	case ffAhead:
		return render.Cell{Text: fmt.Sprintf("ahead +%d → push", res.Ahead), Color: render.Orange}
	case ffBehind:
		return render.Cell{Text: fmt.Sprintf("behind +%d → ff", res.Behind), Color: render.Orange}
	case ffConflict:
		return render.Cell{Text: fmt.Sprintf("conflict: +%d local, +%d origin", res.Ahead, res.Behind), Color: render.Red}
	case ffSkipped:
		return render.Cell{Text: render.Ellipsis("skipped: "+res.Detail, maxDetailWidth), Color: render.Grey}
	default:
		return render.Cell{Text: render.Ellipsis("error: "+res.Detail, maxDetailWidth), Color: render.Red}
	}
}

func printConflict(p *render.Printer, res ffResult) {
	g := "git -C " + shellQuote(res.dir) + " "
	b := res.Branch
	origin := "origin/" + b
	code := func(cmd string) { p.Line(render.Blue, "       %s", cmd) }

	p.Line("", "")
	p.Line(render.Red, "Version conflict: %s (%s) — local %s and %s have diverged", res.FullName, b, b, origin)
	p.Line("", "  local  %s  +%d commits not on origin", shortSHA(res.LocalSHA), res.Ahead)
	p.Line("", "  origin %s  +%d commits not in local", shortSHA(res.RemoteSHA), res.Behind)
	p.Line("", "")
	p.Line(render.Bold, "  1. Merge the histories, resolve conflicts and commit, then push:")
	if !res.CheckedOut {
		code(g + "checkout " + b)
	}
	code(g + "merge " + origin)
	code(g + "push origin " + b)
	p.Line(render.Bold, "  2. Keep local, overwrite the history on origin:")
	code(g + "push --force-with-lease=" + b + ":" + res.RemoteSHA + " origin " + b)
	p.Line(render.Bold, "  3. Keep origin, overwrite the local history (local commits stay in the backup branch):")
	code(g + "branch backup/" + b + "-" + shortSHA(res.LocalSHA) + " " + b)
	if res.CheckedOut {
		code(g + "reset --hard " + origin)
	} else {
		code(g + "branch -f " + b + " " + origin)
	}
	p.Line(render.Grey, "  These commands are only suggestions, git-repos does not run them.")
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func forEachDefault(ctx context.Context, s *session, pairs []matched, opts options, pr *progress, phase string, action branchAction) []ffResult {
	results := make([]ffResult, len(pairs))
	sem := make(chan struct{}, max(opts.jobs, 1))
	pr.setPhase(phase)
	pr.setTotal(len(pairs))

	var wg sync.WaitGroup
	for i, pair := range pairs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			pr.begin(pair.remote.FullName())
			defer func() {
				pr.end()
				<-sem
			}()
			branch := resolveBranch(s.store, s.prov.Name(), pair.remote, opts.refresh)
			name := pair.remote.FullName()
			if pair.originName != "" {
				name += " (origin: " + pair.originName + ")"
			}
			res := action(ctx, pair.local.Path, branch)
			res.FullName = name
			res.Path = shortPath(pair.local.Path)
			results[i] = res
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].FullName < results[j].FullName })
	return results
}

func compareDefault(ctx context.Context, dir, branch string) ffResult {
	res := ffResult{dir: dir, Branch: branch}
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
	remote, err := gitcmd.RevParse(ctx, dir, "refs/remotes/origin/"+branch)
	if errors.Is(err, gitcmd.ErrNoRef) {
		res.Outcome = ffNotFetched
		return res
	}
	if err != nil {
		return res.fail(err)
	}
	res.RemoteSHA = remote
	if local == remote {
		res.Outcome = ffUpToDate
		return res
	}
	if res.Ahead, err = gitcmd.CountCommits(ctx, dir, remote, local); err != nil {
		return res.fail(err)
	}
	if res.Behind, err = gitcmd.CountCommits(ctx, dir, local, remote); err != nil {
		return res.fail(err)
	}
	switch {
	case res.Ahead > 0 && res.Behind > 0:
		res.Outcome = ffConflict
	case res.Ahead > 0:
		res.Outcome = ffAhead
	default:
		res.Outcome = ffBehind
	}
	return res
}

func fastForward(ctx context.Context, dir, branch string) ffResult {
	res := compareDefault(ctx, dir, branch)
	switch res.Outcome {
	case ffNotFetched:
		res.Outcome = ffError
		res.Detail = "origin/" + branch + " not fetched, run update"
		return res
	case ffBehind:
	default:
		return res
	}
	var err error
	if res.CheckedOut {
		err = gitcmd.MergeFFOnly(ctx, dir, "refs/remotes/origin/"+branch)
	} else {
		err = gitcmd.FastForwardBranch(ctx, dir, "origin", branch)
	}
	if err != nil {
		return res.fail(err)
	}
	res.Outcome = ffForwarded
	return res
}

func (r ffResult) fail(err error) ffResult {
	r.Outcome = ffError
	r.Detail = errText(err)
	return r
}
