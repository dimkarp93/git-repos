package app

import (
	"context"
	"flag"
	"sort"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/render"
)

type fetchResult struct {
	FullName string
	Path     string
	Branch   string
	Err      error
}

func updateFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("update", opts)
	fs.BoolVar(&opts.refresh, "refresh", false, "ignore cached default branches")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	return fs
}

func runUpdate(args []string) int {
	opts := options{}
	fs := updateFlags(&opts)
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
	results := fetchAll(s.ctx, s, pairs, opts, s.pr)
	s.spinner.Stop()

	failed := countFetchFailed(results)
	s.printer.Table("Fetch result", []string{"REPOSITORY", "BRANCH", "RESULT", "PATH"}, fetchRows(results))
	s.printer.Line("", "Summary: updated — %d, errors — %d", len(results)-failed, failed)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func countFetchFailed(results []fetchResult) int {
	failed := 0
	for _, res := range results {
		if res.Err != nil {
			failed++
		}
	}
	return failed
}

func fetchRows(results []fetchResult) [][]render.Cell {
	rows := make([][]render.Cell, 0, len(results))
	for _, res := range results {
		rows = append(rows, []render.Cell{
			{Text: res.FullName, Color: render.Bold},
			render.Plain(res.Branch),
			fetchStatus(res.Err),
			{Text: render.Ellipsis(res.Path, maxPathWidth), Color: render.Grey},
		})
	}
	return rows
}

func fetchStatus(err error) render.Cell {
	if err != nil {
		return render.Cell{Text: render.Ellipsis("error: "+errText(err), maxDetailWidth), Color: render.Red}
	}

	return render.Cell{Text: "fetched", Color: render.Grey}
}

func fetchAll(ctx context.Context, s *session, pairs []matched, opts options, pr *progress) []fetchResult {
	results := make([]fetchResult, len(pairs))
	pr.setPhase("fetch")
	pr.setTotal(len(pairs))

	parallel(opts.jobs, len(pairs), func(i int) {
		pr.begin(pairs[i].remote.FullName())
		defer pr.end()
		results[i] = fetchPair(ctx, s, pairs[i], opts)
	})

	sort.Slice(results, func(i, j int) bool { return results[i].FullName < results[j].FullName })
	return results
}

func fetchPair(ctx context.Context, s *session, pair matched, opts options) fetchResult {
	branch := resolveBranch(s.store, s.prov.Name(), pair.remote, opts.refresh)
	res := fetchResult{FullName: pair.label(), Path: shortPath(pair.local.Path), Branch: branch}
	if branch == "" {
		res.Err = errDefaultBranchUnknown
		return res
	}

	res.Err = gitcmd.Fetch(ctx, pair.local.Path, pair.gitRemote(), branch)
	return res
}
