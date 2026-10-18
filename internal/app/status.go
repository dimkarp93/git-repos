package app

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"sort"

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
	opts.filters.registerName(fs)
	return fs
}

type statusSummary struct {
	shown, features, dirty, failed int
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

	rows, sum := statusRows(results, opts.filters)
	if len(rows) == 0 {
		if opts.filters.any() {
			s.printer.Line(render.Grey, "Nothing matched the filter %s.", opts.filters.names())
			return ExitOK
		}
		s.printer.Line(render.Grey, "No local repositories found.")
		return ExitOK
	}

	s.printer.Table("Local repository status", []string{"REPOSITORY", "BRANCH", "MARKS", "PATH"}, rows)
	s.printer.Line("", "%s", sum.line(s.printer, opts.filters))
	if sum.failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func statusRows(results []statusResult, f filters) ([][]render.Cell, statusSummary) {
	rows := make([][]render.Cell, 0, len(results))
	var sum statusSummary
	for _, res := range results {
		if f.matchStatus(res) {
			sum.add(res)
			rows = append(rows, statusRow(res))
		}
	}
	return rows, sum
}

func (s *statusSummary) add(res statusResult) {
	s.shown++
	if res.err != nil {
		s.failed++
		return
	}

	if res.feature {
		s.features++
	}
	if res.dirty {
		s.dirty++
	}
}

func statusRow(res statusResult) []render.Cell {
	branch, marks := statusCells(res)
	return []render.Cell{
		{Text: res.name, Color: render.Bold},
		branch,
		marks,
		{Text: render.Ellipsis(res.path, maxPathWidth), Color: render.Grey},
	}
}

func statusCells(res statusResult) (render.Cell, render.Cell) {
	switch {
	case res.err != nil:
		return render.Cell{Text: render.Ellipsis(errText(res.err), maxDetailWidth), Color: render.Red}, render.Cell{}
	case res.feature:
		return render.Cell{Text: res.branch, Color: render.Blue}, markCell(res)
	default:
		return render.Plain(res.branch), markCell(res)
	}
}

func (s statusSummary) line(p *render.Printer, f filters) string {
	line := fmt.Sprintf("Summary: repositories — %d · %s · %s · errors — %d",
		s.shown,
		p.Colored(render.Magenta, fmt.Sprintf("not on the default branch — %d", s.features)),
		p.Colored(render.Yellow, fmt.Sprintf("with changes — %d", s.dirty)),
		s.failed,
	)
	if f.any() {
		return line + " · filter: " + f.names()
	}
	return line
}

func statusAll(ctx context.Context, locals []scan.Repo, opts options, pr *progress) []statusResult {
	results := make([]statusResult, len(locals))
	pr.setPhase("repository status")
	pr.setTotal(len(locals))

	parallel(opts.jobs, len(locals), func(i int) {
		pr.begin(shortPath(locals[i].Path))
		defer pr.end()
		results[i] = repoStatus(ctx, locals[i].Path)
	})

	sort.Slice(results, func(i, j int) bool { return results[i].path < results[j].path })
	return results
}

func repoStatus(ctx context.Context, path string) statusResult {
	res := statusResult{dir: path, name: filepath.Base(path), path: shortPath(path)}
	res.branch, res.feature = branchState(ctx, path)

	dirty, err := gitcmd.IsDirty(ctx, path)
	if err != nil {
		res.err = err
		return res
	}
	res.dirty = dirty
	return res
}

func branchState(ctx context.Context, path string) (string, bool) {
	branch, err := gitcmd.CurrentBranch(ctx, path)
	if err != nil || branch == "" {
		return "detached", true
	}

	def := gitcmd.DefaultBranch(ctx, path)
	return branch, def != "" && def != branch
}
