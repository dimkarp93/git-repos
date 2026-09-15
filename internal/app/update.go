package app

import (
	"context"
	"sort"
	"sync"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/render"
)

type fetchResult struct {
	FullName string
	Path     string
	Branch   string
	Err      error
}

func runUpdate(args []string) int {
	opts := options{}
	fs := newFlagSet("update", &opts)
	fs.BoolVar(&opts.refresh, "refresh", false, "ignore cached default branches")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
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

	rows := make([][]render.Cell, 0, len(results))
	failed := 0
	for _, res := range results {
		status := render.Cell{Text: "fetched", Color: render.Grey}
		if res.Err != nil {
			failed++
			status = render.Cell{Text: render.Ellipsis("ошибка: "+errText(res.Err), maxDetailWidth), Color: render.Red}
		}
		rows = append(rows, []render.Cell{
			{Text: res.FullName, Color: render.Bold},
			render.Plain(res.Branch),
			status,
			{Text: render.Ellipsis(res.Path, maxPathWidth), Color: render.Grey},
		})
	}
	s.printer.Table("Результат fetch", []string{"РЕПОЗИТОРИЙ", "ВЕТКА", "РЕЗУЛЬТАТ", "ПУТЬ"}, rows)
	s.printer.Line("", "Итог: обновлено — %d, ошибок — %d", len(results)-failed, failed)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func fetchAll(ctx context.Context, s *session, pairs []matched, opts options, pr *progress) []fetchResult {
	results := make([]fetchResult, len(pairs))
	sem := make(chan struct{}, opts.jobs)
	pr.setPhase("fetch")
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
			res := fetchResult{FullName: name, Path: shortPath(pair.local.Path), Branch: branch}
			if branch == "" {
				res.Err = errDefaultBranchUnknown
			} else {
				res.Err = gitcmd.Fetch(ctx, pair.local.Path, "origin", branch)
			}
			results[i] = res
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].FullName < results[j].FullName })
	return results
}
