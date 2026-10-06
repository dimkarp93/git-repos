package app

import (
	"context"
	"flag"
	"fmt"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/render"
)

func pushFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("push", opts)
	fs.BoolVar(&opts.refresh, "refresh", false, "ignore cached default branches")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	return fs
}

func runPush(args []string) int {
	return runBranchAction("push", pushFlags, pushDefault, printPush, args)
}

func printPush(p *render.Printer, results []ffResult) int {
	counts := printBranchTable(p, "Push result", results)
	p.Line("", "Summary: %s · up to date — %d · %s · %s · skipped — %d · %s",
		p.Colored(render.Green, fmt.Sprintf("pushed — %d", counts[ffPushed])),
		counts[ffUpToDate],
		p.Colored(render.Orange, fmt.Sprintf("behind — %d", counts[ffBehind])),
		p.Colored(render.Red, fmt.Sprintf("conflicts — %d", counts[ffConflict])),
		counts[ffSkipped],
		p.Colored(render.Red, fmt.Sprintf("errors — %d", counts[ffError])),
	)
	return finishBranchResults(p, results, counts)
}

func pushDefault(ctx context.Context, dir, remoteName, branch string) ffResult {
	res := compareDefault(ctx, dir, remoteName, branch)
	if res.Outcome != ffAhead && res.Outcome != ffNotFetched {
		return res
	}
	upstream, err := gitcmd.Upstream(ctx, dir, branch)
	if err != nil {
		return res.fail(err)
	}
	res.UpstreamSet = upstream == ""
	if err := gitcmd.PushBranch(ctx, dir, remoteName, branch, res.UpstreamSet); err != nil {
		return res.fail(err)
	}
	res.Outcome = ffPushed
	return res
}
