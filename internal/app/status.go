package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/git-repos/internal/scan"
)

type statusResult struct {
	dir     string
	name    string
	path    string
	branch  string
	feature bool
	dirty   bool
	err     error
}

func runStatus(args []string) int {
	opts := options{}
	fs := newFlagSet("status", &opts)
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	opts.filters.registerStatus(fs)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	s, err := startLocal(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(2)
	s.spinner.Start(s.pr.label)
	locals, err := scanLocal(s.cfg, opts, s.roots, s.pr, s.store)
	if err != nil {
		return fail(err)
	}
	results := statusAll(s.ctx, locals, opts, s.pr)
	s.spinner.Stop()

	rows := make([][]render.Cell, 0, len(results))
	features, dirty, failed, shown := 0, 0, 0, 0
	for _, res := range results {
		if !opts.filters.matchStatus(res) {
			continue
		}
		shown++
		branch := render.Plain(res.branch)
		feature := render.Cell{}
		develop := render.Cell{}
		switch {
		case res.err != nil:
			failed++
			branch = render.Cell{Text: render.Ellipsis(errText(res.err), maxDetailWidth), Color: render.Red}
		default:
			if res.feature {
				features++
				branch = render.Cell{Text: res.branch, Color: render.Blue}
				feature = render.Cell{Text: "[feature]", Color: render.Magenta}
			}
			if res.dirty {
				dirty++
				develop = render.Cell{Text: "[in develop]", Color: render.Yellow}
			}
		}
		rows = append(rows, []render.Cell{
			{Text: res.name, Color: render.Bold},
			branch,
			feature,
			develop,
			{Text: render.Ellipsis(res.path, maxPathWidth), Color: render.Grey},
		})
	}
	if len(rows) == 0 {
		if opts.filters.any() {
			s.printer.Line(render.Grey, "Под фильтр %s ничего не попало.", opts.filters.names())
			return ExitOK
		}
		s.printer.Line(render.Grey, "Локальные репозитории не найдены.")
		return ExitOK
	}
	s.printer.Table("Состояние локальных репозиториев", []string{"РЕПОЗИТОРИЙ", "ВЕТКА", "СОСТОЯНИЕ", "ИЗМЕНЕНИЯ", "ПУТЬ"}, rows)
	line := fmt.Sprintf("Итог: репозиториев — %d · %s · %s · ошибок — %d",
		shown,
		s.printer.Colored(render.Magenta, fmt.Sprintf("не в дефолтной ветке — %d", features)),
		s.printer.Colored(render.Yellow, fmt.Sprintf("с изменениями — %d", dirty)),
		failed,
	)
	if opts.filters.any() {
		line += " · фильтр: " + opts.filters.names()
	}
	s.printer.Line("", "%s", line)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func statusAll(ctx context.Context, locals []scan.Repo, opts options, pr *progress) []statusResult {
	results := make([]statusResult, len(locals))
	sem := make(chan struct{}, opts.jobs)
	pr.setPhase("статус репозиториев")
	pr.setTotal(len(locals))
	var wg sync.WaitGroup
	for i, local := range locals {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			pr.begin(shortPath(local.Path))
			defer func() {
				pr.end()
				<-sem
			}()
			results[i] = repoStatus(ctx, local.Path)
		}()
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].path < results[j].path })
	return results
}

func repoStatus(ctx context.Context, path string) statusResult {
	res := statusResult{dir: path, name: filepath.Base(path), path: shortPath(path)}
	branch, err := gitcmd.CurrentBranch(ctx, path)
	if err != nil || branch == "" {
		res.branch = "detached"
		res.feature = true
	} else {
		res.branch = branch
		if def := gitcmd.DefaultBranch(ctx, path); def != "" && def != branch {
			res.feature = true
		}
	}
	dirty, err := gitcmd.IsDirty(ctx, path)
	if err != nil {
		res.err = err
		return res
	}
	res.dirty = dirty
	return res
}
