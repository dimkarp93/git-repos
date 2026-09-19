package app

import (
	"context"
	"flag"
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

type statusMark struct {
	name  string
	color string
	on    func(statusResult) bool
	want  func(filters) bool
}

func statusMarks() []statusMark {
	return []statusMark{
		{"feature", render.Magenta, func(r statusResult) bool { return r.feature }, func(f filters) bool { return f.feature }},
		{"in-develop", render.Yellow, func(r statusResult) bool { return r.dirty }, func(f filters) bool { return f.inDevelop }},
		{"hotfix", render.Red, func(r statusResult) bool { return !r.feature && r.dirty }, func(f filters) bool { return f.hotfix }},
		{"pushable", render.Green, func(r statusResult) bool { return r.feature && !r.dirty }, func(f filters) bool { return f.pushable }},
		{"wip", render.Blue, func(r statusResult) bool { return r.feature || r.dirty }, func(f filters) bool { return f.wip }},
	}
}

func markCell(res statusResult) render.Cell {
	marks := statusMarks()
	parts := make([]render.Cell, 0, len(marks))
	for _, m := range marks {
		if m.on(res) {
			parts = append(parts, render.Cell{Text: "[" + m.name + "]", Color: m.color})
		}
	}
	return render.Multi(parts...)
}

func statusFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("status", opts)
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	opts.filters.registerStatus(fs)
	return fs
}

func runStatus(args []string) int {
	opts := options{}
	fs := statusFlags(&opts)
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
		marks := render.Cell{}
		switch {
		case res.err != nil:
			failed++
			branch = render.Cell{Text: render.Ellipsis(errText(res.err), maxDetailWidth), Color: render.Red}
		default:
			if res.feature {
				features++
				branch = render.Cell{Text: res.branch, Color: render.Blue}
			}
			if res.dirty {
				dirty++
			}
			marks = markCell(res)
		}
		rows = append(rows, []render.Cell{
			{Text: res.name, Color: render.Bold},
			branch,
			marks,
			{Text: render.Ellipsis(res.path, maxPathWidth), Color: render.Grey},
		})
	}
	if len(rows) == 0 {
		if opts.filters.any() {
			s.printer.Line(render.Grey, "Nothing matched the filter %s.", opts.filters.names())
			return ExitOK
		}
		s.printer.Line(render.Grey, "No local repositories found.")
		return ExitOK
	}
	s.printer.Table("Local repository status", []string{"REPOSITORY", "BRANCH", "MARKS", "PATH"}, rows)
	line := fmt.Sprintf("Summary: repositories — %d · %s · %s · errors — %d",
		shown,
		s.printer.Colored(render.Magenta, fmt.Sprintf("not on the default branch — %d", features)),
		s.printer.Colored(render.Yellow, fmt.Sprintf("with changes — %d", dirty)),
		failed,
	)
	if opts.filters.any() {
		line += " · filter: " + opts.filters.names()
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
	pr.setPhase("repository status")
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
