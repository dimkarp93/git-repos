package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/dimkarp93/git-repos/internal/cache"
	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/provider/github"
	"github.com/dimkarp93/git-repos/internal/render"
)

const (
	ExitOK      = 0
	ExitDiff    = 1
	ExitFailure = 2
)

const cacheTTL = 7 * 24 * time.Hour

type options struct {
	roots      repeatable
	providerID string
	protocol   string
	fetch      bool
	all        bool
	refresh    bool
	dryRun     bool
	assumeYes  bool
	into       string
	depth      int
	jobs       int
	noCache    bool
	clearCache bool
	noColor    bool
	noProgress bool
	forceColor bool
	asJSON     bool
	filters    filters
	timeout    time.Duration
}

type repeatable []string

func (r *repeatable) String() string { return strings.Join(*r, ",") }

func (r *repeatable) Set(v string) error {
	*r = append(*r, v)
	return nil
}

type command struct {
	name    string
	summary string
	run     func(args []string) int
	flags   func(opts *options) *flag.FlagSet
	hidden  bool
}

func commands() []command {
	return []command{
		{name: "diff", summary: "show differences between local repositories and the account", run: runDiff, flags: diffFlags},
		{name: "status", summary: "show the branch and working tree state of local repositories", run: runStatus, flags: statusFlags},
		{name: "do", summary: "run a command inside every selected repository", run: runDo, flags: doFlags},
		{name: "update", summary: "fetch the default branch in every repository", run: runUpdate, flags: updateFlags},
		{name: "ff", summary: "fast-forward the default branch to the fetched origin branch", run: runFF, flags: ffFlags},
		{name: "push", summary: "push the default branch when it is ahead, setting the upstream if missing", run: runPush, flags: pushFlags},
		{name: "rename", summary: "rename a repository locally and on the account", run: runRename, flags: renameFlags},
		{name: "sync", summary: "create missing repositories on both sides", run: runSync, flags: syncFlags},
		{name: "clean-local", summary: "delete local repositories without a remote", run: runCleanLocal, flags: cleanLocalFlags},
		{name: "clean-remote", summary: "delete remote repositories without a local copy", run: runCleanRemote, flags: cleanRemoteFlags},
		{name: "completion", summary: "print the shell completion script for bash or zsh", run: runCompletion, flags: completionFlags},
		{name: "install-completions", summary: "install the completion script into bash and zsh", run: runInstallCompletions, flags: installCompletionsFlags},
		{name: "uninstall-completions", summary: "remove the completion script from bash and zsh", run: runUninstallCompletions, flags: uninstallCompletionsFlags},
		{name: completeCommand, run: runCompleteHidden, hidden: true},
	}
}

func Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "git-repos: no command given")
		fmt.Fprintln(os.Stderr)
		usage(os.Stderr)
		return ExitFailure
	}
	name := args[0]
	if name == "help" || name == "-h" || name == "--help" {
		usage(os.Stdout)
		return ExitOK
	}
	if strings.HasPrefix(name, "-") {
		fmt.Fprintf(os.Stderr, "git-repos: no command given, got flag %s\n\n", name)
		usage(os.Stderr)
		return ExitFailure
	}
	args = args[1:]
	for _, cmd := range commands() {
		if cmd.name == name {
			return cmd.run(args)
		}
	}
	fmt.Fprintf(os.Stderr, "git-repos: unknown command %q\n\n", name)
	usage(os.Stderr)
	return ExitFailure
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: git-repos <command> [flags]")
	fmt.Fprintln(w, "\ncommands:")
	width := 0
	for _, cmd := range commands() {
		if !cmd.hidden && len(cmd.name) > width {
			width = len(cmd.name)
		}
	}
	for _, cmd := range commands() {
		if cmd.hidden {
			continue
		}
		fmt.Fprintf(w, "  %-*s %s\n", width, cmd.name, cmd.summary)
	}
	fmt.Fprintln(w, "\n"+heading("common flags:"))
	var opts options
	fs := newFlagSet("<command>", &opts)
	printFlags(w, fs, func(string) bool { return true })
	fmt.Fprintln(w, "\ngit-repos <command> -h shows the flags of that command")
	fmt.Fprintln(w, "git-repos install-completions sets up completion for bash and zsh")
}

func newFlagSet(name string, opts *options) *flag.FlagSet {
	fs := flag.NewFlagSet("git-repos "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Var(&opts.roots, "C", "root directory to scan (repeatable)")
	fs.StringVar(&opts.providerID, "provider", "", "provider to query (default from config, else github)")
	fs.IntVar(&opts.depth, "depth", 0, "maximum scan depth, 0 means unlimited")
	fs.IntVar(&opts.jobs, "jobs", 8, "number of repositories inspected in parallel")
	fs.BoolVar(&opts.noCache, "no-cache", false, "do not read from or write to the cache")
	fs.BoolVar(&opts.clearCache, "clear-cache", false, "delete the cache file before running")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable colored output")
	fs.BoolVar(&opts.forceColor, "color", false, "force colored output")
	fs.DurationVar(&opts.timeout, "timeout", 15*time.Minute, "overall timeout")
	fs.Usage = func() { commandUsage(fs.Output(), fs, name) }
	return fs
}

func commandUsage(w io.Writer, fs *flag.FlagSet, name string) {
	fmt.Fprintf(w, "usage: git-repos %s [flags]\n", name)
	if hasFilters(fs) {
		fmt.Fprintln(w, "\n"+heading("filters (several filters are combined with OR):"))
		printFlags(w, fs, isFilterFlag)
	}
	fmt.Fprintln(w, "\n"+heading("flags:"))
	printFlags(w, fs, func(flagName string) bool { return !isFilterFlag(flagName) })
}

func heading(text string) string {
	return render.Colorize(render.ColorEnabled(false, false, os.Stderr), render.Bold, text)
}

func hasFilters(fs *flag.FlagSet) bool {
	found := false
	fs.VisitAll(func(f *flag.Flag) {
		if isFilterFlag(f.Name) {
			found = true
		}
	})
	return found
}

func printFlags(w io.Writer, fs *flag.FlagSet, want func(name string) bool) {
	width := 0
	fs.VisitAll(func(f *flag.Flag) {
		if n := len(flagLabel(f)); n > width {
			width = n
		}
	})
	fs.VisitAll(func(f *flag.Flag) {
		if !want(f.Name) {
			return
		}
		_, usage := flag.UnquoteUsage(f)
		label := flagLabel(f)
		fmt.Fprintf(w, "  %-*s  %s\n", width, label, usage)
	})
}

func flagLabel(f *flag.Flag) string {
	kind, _ := flag.UnquoteUsage(f)
	label := "-" + f.Name
	if kind != "" {
		label += " " + kind
	}
	if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
		label += " (" + f.DefValue + ")"
	}
	return label
}

func parseFlags(fs *flag.FlagSet, args []string) (bool, int) {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, ExitOK
		}
		return false, ExitFailure
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "git-repos: unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return false, ExitFailure
	}
	return true, ExitOK
}

type session struct {
	ctx     context.Context
	cancel  context.CancelFunc
	stop    context.CancelFunc
	cfg     config.Config
	opts    options
	roots   []string
	prov    provider.Provider
	store   *cache.Cache
	printer *render.Printer
	pr      *progress
	spinner *render.Spinner
}

func (s *session) close() {
	s.spinner.Stop()
	s.stop()
	s.cancel()
	if err := s.store.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "git-repos: cache: %v\n", err)
	}
}

func start(opts options) (*session, error) {
	s, err := startLocal(opts)
	if err != nil {
		return nil, err
	}
	prov, err := newProvider(s.cfg.Provider)
	if err != nil {
		s.close()
		return nil, err
	}
	if reporter, ok := prov.(provider.ProgressReporter); ok {
		reporter.SetProgress(s.pr)
	}
	s.prov = prov
	return s, nil
}

func startLocal(opts options) (*session, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if opts.providerID != "" {
		cfg.Provider = opts.providerID
	}
	if opts.protocol != "" {
		cfg.Protocol = opts.protocol
	}
	if opts.jobs < 1 {
		opts.jobs = 1
	}
	roots, err := resolveRoots(opts.roots, cfg.Roots)
	if err != nil {
		return nil, err
	}
	if opts.clearCache {
		if err := cache.Remove(cfg.CacheDir); err != nil {
			fmt.Fprintf(os.Stderr, "git-repos: cache: %v\n", err)
		}
	}
	store := cache.Off()
	if !opts.noCache {
		store, err = cache.Open(cfg.CacheDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "git-repos: cache: %v\n", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	if opts.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), opts.timeout)
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	return &session{
		ctx:     ctx,
		cancel:  cancel,
		stop:    stop,
		cfg:     cfg,
		opts:    opts,
		roots:   roots,
		store:   store,
		printer: newPrinter(opts),
		pr:      &progress{color: render.ColorEnabled(opts.forceColor, opts.noColor, os.Stderr)},
		spinner: render.NewSpinner(os.Stderr, !opts.noProgress && render.IsTerminal(os.Stderr)),
	}, nil
}

func newPrinter(opts options) *render.Printer {
	p := render.NewPrinter(os.Stdout, render.ColorEnabled(opts.forceColor, opts.noColor, os.Stdout))
	p.Width = render.TerminalWidth(os.Stdout)
	return p
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "git-repos: %v\n", err)
	return ExitFailure
}

func resolveRoots(flagRoots, configRoots []string) ([]string, error) {
	roots := flagRoots
	if len(roots) == 0 {
		roots = configRoots
	}
	if len(roots) == 0 {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		roots = []string{cwd}
	}
	out := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, root := range roots {
		root = config.ExpandPath(root)
		if root == "" || seen[root] {
			continue
		}
		if _, err := os.Stat(root); err != nil {
			return nil, fmt.Errorf("root %s: %w", root, err)
		}
		seen[root] = true
		out = append(out, root)
	}
	sort.Strings(out)
	return out, nil
}

func newProvider(id string) (provider.Provider, error) {
	switch id {
	case "", "github":
		token := firstEnv("GITHUB_TOKEN", "GH_TOKEN")
		if token == "" {
			return nil, fmt.Errorf("%w: set GITHUB_TOKEN", provider.ErrNoToken)
		}
		opts := []github.Option{}
		if base := firstEnv("GITHUB_API_URL"); base != "" {
			opts = append(opts, github.WithBaseURL(base))
		}
		if host := firstEnv("GITHUB_HOST"); host != "" {
			opts = append(opts, github.WithHost(host))
		}
		return github.New(token, opts...), nil
	default:
		return nil, fmt.Errorf("%w: %s", provider.ErrNoProvider, id)
	}
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v
		}
	}
	return ""
}
