package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dimkarp93/git-repos/internal/render"
)

func doFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("do", opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print what would be run and change nothing")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	opts.filters.registerAll(fs)
	return fs
}

func runDo(args []string) int {
	opts := options{}
	fs := doFlags(&opts)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitFailure
	}
	action := fs.Args()
	if len(action) == 0 {
		fmt.Fprintln(os.Stderr, "git-repos: no command given, for example: git-repos do --in-develop git status -s")
		return ExitFailure
	}
	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if !explicit["timeout"] {
		opts.timeout = 0
	}
	if explicit["jobs"] {
		fmt.Fprintln(os.Stderr, "git-repos: do always runs sequentially, -jobs is ignored")
	}

	s, err := startFor(opts, opts.filters.anyView())
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(doPhases(opts.filters))
	s.spinner.Start(s.pr.label)
	targets, err := selectRepos(s, opts)
	s.spinner.Stop()
	if err != nil {
		return fail(err)
	}
	if len(targets) == 0 {
		if opts.filters.any() {
			s.printer.Line(render.Grey, "Nothing matched the filter %s.", opts.filters.names())
		} else {
			s.printer.Line(render.Grey, "No local repositories found.")
		}
		return ExitOK
	}

	script := shellScript(action)
	if opts.dryRun {
		for _, dir := range targets {
			s.printer.Line("", "cd %s && %s", shortPath(dir), script)
		}
		return ExitOK
	}
	return execAll(s, targets, script)
}

func execAll(s *session, targets []string, script string) int {
	var failedDirs []string
	for i, dir := range targets {
		if s.ctx.Err() != nil {
			fmt.Fprintf(os.Stderr, "git-repos: interrupted, %d repositories left\n", len(targets)-i)
			break
		}
		s.printer.Line("", "%s %s",
			s.printer.Colored(render.Bold, fmt.Sprintf("[%d/%d] :: %s", i+1, len(targets), filepath.Base(dir))),
			s.printer.Colored(render.Grey, shortPath(dir)))
		if code := runScript(dir, script); code != 0 {
			failedDirs = append(failedDirs, shortPath(dir))
			fmt.Fprintf(os.Stderr, "git-repos: exit %d\n", code)
		}
	}
	line := fmt.Sprintf("Summary: done — %d, errors — %d", len(targets)-len(failedDirs), len(failedDirs))
	s.printer.Line("", "%s", line)
	if len(failedDirs) > 0 {
		s.printer.Line(render.Red, "Failed: %s", strings.Join(failedDirs, ", "))
		return ExitFailure
	}
	return ExitOK
}

func runScript(dir, script string) int {
	cmd := exec.Command("sh", "-c", script)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "GIT_REPOS_NAME="+filepath.Base(dir), "GIT_REPOS_PATH="+dir)
	err := cmd.Run()
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	fmt.Fprintf(os.Stderr, "git-repos: %v\n", err)
	return 1
}

func shellScript(action []string) string {
	if len(action) == 1 {
		return action[0]
	}
	quoted := make([]string, 0, len(action))
	for _, arg := range action {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellQuote(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n'\"\\$`&|;<>()*?[]#~!{}") {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}

func doPhases(f filters) int {
	if !f.anyView() {
		if f.anyStatus() {
			return 2
		}
		return 1
	}
	if f.anyStatus() {
		return 5
	}
	return 4
}

func selectRepos(s *session, opts options) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	keep := func(dir string) {
		if dir == "" || seen[dir] {
			return
		}
		seen[dir] = true
		out = append(out, dir)
	}

	if !opts.filters.anyView() {
		locals, err := scanLocal(s.cfg, opts, s.roots, s.pr, s.store)
		if err != nil {
			return nil, err
		}
		if !opts.filters.anyStatus() {
			for _, local := range locals {
				keep(local.Path)
			}
			sort.Strings(out)
			return out, nil
		}
		for _, res := range statusAll(s.ctx, locals, opts, s.pr) {
			if opts.filters.matchStatus(res) {
				keep(res.dir)
			}
		}
		sort.Strings(out)
		return out, nil
	}

	pairs, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		return nil, err
	}
	if opts.filters.local {
		for _, item := range inv.localOnly {
			keep(item.Path)
		}
	}
	if opts.filters.remote && len(inv.remoteOnly) > 0 {
		fmt.Fprintf(os.Stderr, "git-repos: %d repositories exist only on the remote with no local copy — skipped\n", len(inv.remoteOnly))
	}
	for _, res := range inspectAll(s.ctx, s.prov, s.store, pairs, opts, s.pr) {
		if opts.filters.matchResult(res) {
			keep(res.dir)
		}
	}
	if opts.filters.anyStatus() {
		for _, res := range statusAll(s.ctx, inv.locals, opts, s.pr) {
			if opts.filters.matchStatus(res) {
				keep(res.dir)
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func startFor(opts options, needProvider bool) (*session, error) {
	if needProvider {
		return start(opts)
	}
	return startLocal(opts)
}
