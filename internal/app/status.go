package app

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/multi"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/git-repos/internal/scan"
)

type statusResult struct {
	dir       string
	repo      string
	name      string
	path      string
	workspace string
	branch    string
	feature   bool
	dirty     bool
	real      bool
	err       error
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
		{"pushable", render.Green, func(r statusResult) bool { return r.feature && !r.dirty && !r.real }, func(f filters) bool { return f.pushable }},
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
	opts.filters.registerWorktree(fs)
	fs.StringVar(&opts.group, "group", "", "print only the groups with row counts: repo, branch or worktree")
	return fs
}

type statusSummary struct {
	shown, repos, workspaces, features, dirty, failed int
}

func runStatus(args []string) int {
	opts := options{}
	fs := statusFlags(&opts)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	var group statusGroup
	if opts.group != "" {
		var err error
		if group, err = lookupStatusGroup(opts.group); err != nil {
			return fail(err)
		}
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
	results := statusAll(s.ctx, s.multi, locals, opts, s.pr)
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

	if opts.group != "" {
		s.printer.Table("Local repository status by "+group.name, []string{group.header, "COUNT"}, groupRows(countGroups(results, opts.filters, group)))
	} else {
		s.printer.Table("Local repository status", []string{"REPOSITORY", "WORKSPACE", "BRANCH", "MARKS", "PATH"}, rows)
		s.printer.Line("", "%s", sum.line(s.printer, opts.filters))
	}
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
	if res.real {
		s.workspaces++
	} else {
		s.repos++
	}
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
		render.Plain(res.workspace),
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
	line := fmt.Sprintf("Summary: repositories — %d · workspaces — %d · %s · %s · errors — %d",
		s.repos,
		s.workspaces,
		p.Colored(render.Magenta, fmt.Sprintf("not on the default branch — %d", s.features)),
		p.Colored(render.Yellow, fmt.Sprintf("with changes — %d", s.dirty)),
		s.failed,
	)
	if f.any() {
		return line + " · filter: " + f.names()
	}
	return line
}

func statusAll(ctx context.Context, list multi.Lister, locals []scan.Repo, opts options, pr *progress) []statusResult {
	groups := make([][]statusResult, len(locals))
	pr.setPhase("repository status")
	pr.setTotal(len(locals))

	parallel(opts.jobs, len(locals), func(i int) {
		pr.begin(shortPath(locals[i].Path))
		defer pr.end()
		groups[i] = repoStatus(ctx, list, locals[i].Path)
	})

	results := uniqueRepos(groups)
	sort.SliceStable(results, func(i, j int) bool { return statusLess(results[i], results[j]) })
	return results
}

func uniqueRepos(groups [][]statusResult) []statusResult {
	seen := map[string]bool{}
	var out []statusResult
	for _, group := range groups {
		if len(group) == 0 || seen[group[0].repo] {
			continue
		}
		seen[group[0].repo] = true
		out = append(out, group...)
	}
	return out
}

func statusLess(a, b statusResult) bool {
	if a.repo != b.repo {
		return a.repo < b.repo
	}
	if a.real != b.real {
		return !a.real
	}
	return a.workspace < b.workspace
}

func repoStatus(ctx context.Context, list multi.Lister, path string) []statusResult {
	workspaces, err := list(ctx, path)
	if err != nil {
		return []statusResult{failedStatus(path, err)}
	}

	idx := slices.IndexFunc(workspaces, func(ws multi.Workspace) bool { return ws.Kind == multi.Main })
	if idx < 0 {
		return []statusResult{failedStatus(path, fmt.Errorf("%s ls: no main workspace", multi.Binary))}
	}

	main := mainStatus(ctx, workspaces[idx])
	out := []statusResult{main}
	for _, ws := range workspaces {
		if ws.Kind == multi.Real && !ws.Missing {
			out = append(out, realStatus(main, ws))
		}
	}
	return out
}

func failedStatus(path string, err error) statusResult {
	return statusResult{dir: path, repo: path, name: filepath.Base(path), path: shortPath(path), err: err}
}

func mainStatus(ctx context.Context, ws multi.Workspace) statusResult {
	res := statusResult{
		dir:       ws.Path,
		repo:      ws.Path,
		name:      filepath.Base(ws.Path),
		path:      shortPath(ws.Path),
		workspace: ws.Name,
	}
	res.branch, res.feature = branchState(ctx, ws.Path)

	dirty, err := gitcmd.IsDirty(ctx, ws.Path)
	if err != nil {
		res.err = err
		return res
	}
	res.dirty = dirty
	return res
}

func realStatus(main statusResult, ws multi.Workspace) statusResult {
	branch := ws.Branch
	if branch == "" {
		branch = "detached"
	}
	return statusResult{
		dir:       ws.Path,
		repo:      main.repo,
		name:      main.name,
		path:      shortPath(ws.Path),
		workspace: ws.Name,
		branch:    branch,
		feature:   true,
		real:      true,
	}
}

func branchState(ctx context.Context, path string) (string, bool) {
	branch, err := gitcmd.CurrentBranch(ctx, path)
	if err != nil || branch == "" {
		return "detached", true
	}

	def := gitcmd.DefaultBranch(ctx, path)
	return branch, def != "" && def != branch
}
