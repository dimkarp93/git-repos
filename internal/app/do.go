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
	"github.com/dimkarp93/git-repos/internal/scan"
)

func doFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("do", opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print what would be run and change nothing")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	opts.filters.registerAll(fs)
	return fs
}

func runDo(args []string) int {
	opts, action, code, ok := parseDo(args)
	if !ok {
		return code
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
		printNoTargets(s.printer, opts.filters)
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

func parseDo(args []string) (options, []string, int, bool) {
	opts := options{}
	fs := doFlags(&opts)
	if err := fs.Parse(args); err != nil {
		return opts, nil, parseExit(err), false
	}

	action := fs.Args()
	if len(action) == 0 {
		fmt.Fprintln(os.Stderr, "git-repos: no command given, for example: git-repos do --in-develop git status -s")
		return opts, nil, ExitFailure, false
	}

	explicit := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { explicit[f.Name] = true })
	if !explicit["timeout"] {
		opts.timeout = 0
	}
	if explicit["jobs"] {
		fmt.Fprintln(os.Stderr, "git-repos: do always runs sequentially, -jobs is ignored")
	}
	return opts, action, ExitOK, true
}

func parseExit(err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return ExitOK
	}

	return ExitFailure
}

func printNoTargets(p *render.Printer, f filters) {
	if f.any() {
		p.Line(render.Grey, "Nothing matched the filter %s.", f.names())
		return
	}

	p.Line(render.Grey, "No local repositories found.")
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

type dirSet struct {
	seen map[string]bool
	out  []string
}

func selectRepos(s *session, opts options) ([]string, error) {
	set := &dirSet{seen: map[string]bool{}}
	var err error
	if opts.filters.anyView() {
		err = providerTargets(s, opts, set)
	} else {
		err = localTargets(s, opts, set)
	}
	if err != nil {
		return nil, err
	}

	sort.Strings(set.out)
	return set.out, nil
}

func (d *dirSet) add(dir string) {
	if dir == "" || d.seen[dir] {
		return
	}

	d.seen[dir] = true
	d.out = append(d.out, dir)
}

func (d *dirSet) addStatus(s *session, locals []scan.Repo, opts options) {
	for _, res := range statusAll(s.ctx, locals, opts, s.pr) {
		if opts.filters.matchStatus(res) {
			d.add(res.dir)
		}
	}
}

func localTargets(s *session, opts options, set *dirSet) error {
	locals, err := scanLocal(s.cfg, opts, s.roots, s.pr, s.store)
	if err != nil {
		return err
	}

	if opts.filters.anyStatus() {
		set.addStatus(s, locals, opts)
		return nil
	}
	for _, local := range locals {
		set.add(local.Path)
	}
	return nil
}

func providerTargets(s *session, opts options, set *dirSet) error {
	pairs, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		return err
	}

	if opts.filters.local {
		for _, item := range inv.localOnly {
			set.add(item.Path)
		}
	}
	if opts.filters.remote && len(inv.remoteOnly) > 0 {
		fmt.Fprintf(os.Stderr, "git-repos: %d repositories exist only on the remote with no local copy — skipped\n", len(inv.remoteOnly))
	}

	for _, res := range inspectAll(s.ctx, s.prov, s.store, pairs, opts, s.pr) {
		if opts.filters.matchResult(res) {
			set.add(res.dir)
		}
	}
	if opts.filters.anyStatus() {
		set.addStatus(s, inv.locals, opts)
	}
	return nil
}

func isSetDir(dir string) bool {
	panic("not implemented")
}

func statusDirs(s *session, locals []scan.Repo, opts options) []string {
	panic("not implemented")
}

func localDirs(s *session, opts options) ([]string, error) {
	panic("not implemented")
}

func providerDirs(s *session, opts options) ([]string, error) {
	panic("not implemented")
}

func localOnlyDirs(inv inventory, opts options) []string {
	panic("not implemented")
}

func repoPath(local scan.Repo) string {
	panic("not implemented")
}

func localOnlyPath(item LocalOnly) string {
	panic("not implemented")
}

func statusResultDir(res statusResult) string {
	panic("not implemented")
}

func inspectResultDir(res Result) string {
	panic("not implemented")
}

func startFor(opts options, needProvider bool) (*session, error) {
	if needProvider {
		return start(opts)
	}
	return startLocal(opts)
}
