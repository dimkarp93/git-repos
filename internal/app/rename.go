package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
	"github.com/dimkarp93/git-repos/internal/scan"
	"github.com/dimkarp93/git-repos/internal/terminal"
	"github.com/dimkarp93/utils/pkg/collections"
)

const (
	maxRenameCandidates = 20
)

func renameFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("rename [<old-name> <new-name>]", opts)
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	return fs
}

func runRename(args []string) int {
	opts, rest, code, ok := parseRename(args)
	if !ok {
		return code
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(3)
	s.spinner.Start(s.pr.label)
	_, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	s.spinner.Stop()
	if err != nil {
		return fail(err)
	}

	pair, newName, code, ok := s.renameTarget(inv, rest)
	if !ok {
		return code
	}

	g, err := newGuard(s.ctx, inv.pairs)
	if err != nil {
		return fail(err)
	}
	if row, blocked := g.check(pair.local.Path); blocked {
		printSkipped(s.printer, []skippedRepo{row})
		return ExitFailure
	}

	if err := s.executeRename(pair, newName); err != nil {
		return fail(err)
	}
	return ExitOK
}

func parseRename(args []string) (options, []string, int, bool) {
	opts := options{}
	fs := renameFlags(&opts)
	if err := fs.Parse(args); err != nil {
		return opts, nil, parseExit(err), false
	}

	rest := fs.Args()
	if len(rest) != 0 && len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "git-repos: rename takes either no arguments or <old-name> <new-name>")
		return opts, nil, ExitFailure, false
	}
	if len(rest) == 0 && !(render.IsTerminal(os.Stdin) && render.IsTerminal(os.Stdout)) {
		return opts, nil, fail(errors.New("no terminal for the interactive mode, use: git-repos rename <old-name> <new-name>")), false
	}
	return opts, rest, ExitOK, true
}

func (s *session) renameTarget(inv inventory, rest []string) (matched, string, int, bool) {
	if len(rest) == 0 {
		return s.pickRenameTarget(inv)
	}

	pair, err := findRenamePair(inv, rest[0])
	if err == nil {
		err = checkNewName(inv, pair, rest[1])
	}
	if err != nil {
		return matched{}, "", fail(err), false
	}
	return pair, rest[1], ExitOK, true
}

func (s *session) pickRenameTarget(inv inventory) (matched, string, int, bool) {
	candidates := renameCandidates(s.ctx, inv)
	if len(candidates) == 0 {
		s.printer.Line(render.Grey, "No repositories that exist both locally and on %s.", s.prov.Name())
		return matched{}, "", ExitOK, false
	}

	oldName, newName, cancelled, err := pickRename(candidates, inv)
	if err != nil {
		return matched{}, "", fail(err), false
	}
	if cancelled {
		s.printer.Line("", "Operation cancelled")
		return matched{}, "", ExitOK, false
	}

	pair, err := findRenamePair(inv, oldName)
	if err != nil {
		return matched{}, "", fail(err), false
	}
	return pair, newName, ExitOK, true
}

func validateRepoName(name string) error {
	if name == "" {
		return errors.New("the name is empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%q is not a valid name", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("%q contains %q, allowed: letters, digits, '-', '_', '.'", name, r)
		}
	}
	return nil
}

func findRenamePair(inv inventory, oldName string) (matched, error) {
	locals := localsNamed(inv, oldName)
	remotes := remotesNamed(inv, oldName)
	if err := uniqueRenamePair(oldName, locals, remotes); err != nil {
		return matched{}, err
	}

	for _, pair := range inv.pairs {
		if pair.local.Path == locals[0] && strings.EqualFold(pair.remote.FullName(), remotes[0].FullName()) {
			return pair, nil
		}
	}
	return matched{}, fmt.Errorf("%s is not linked to %s through origin", shortPath(locals[0]), remotes[0].FullName())
}

func localsNamed(inv inventory, name string) []string {
	named := collections.Filter(inv.locals, func(local scan.Repo) bool {
		return strings.EqualFold(filepath.Base(local.Path), name)
	})
	return collections.Map(named, repoPath)
}

func remotesNamed(inv inventory, name string) []provider.Repo {
	return collections.Filter(inv.remotes, func(repo provider.Repo) bool {
		return strings.EqualFold(repo.Name, name)
	})
}

func uniqueRenamePair(name string, locals []string, remotes []provider.Repo) error {
	switch {
	case len(locals) == 0:
		return fmt.Errorf("no local repository named %q", name)
	case len(locals) > 1:
		return fmt.Errorf("%d local repositories named %q: %s", len(locals), name, joinShort(locals))
	case len(remotes) == 0:
		return fmt.Errorf("no remote repository named %q", name)
	case len(remotes) > 1:
		return fmt.Errorf("%d remote repositories named %q: %s", len(remotes), name, joinFullNames(remotes))
	default:
		return nil
	}
}

func joinFullNames(repos []provider.Repo) string {
	return strings.Join(collections.Map(repos, provider.Repo.FullName), ", ")
}

func joinShort(paths []string) string {
	return strings.Join(collections.Map(paths, shortPath), ", ")
}

func checkNewName(inv inventory, pair matched, newName string) error {
	if err := validateRepoName(newName); err != nil {
		return err
	}
	for _, local := range inv.locals {
		if strings.EqualFold(filepath.Base(local.Path), newName) {
			return fmt.Errorf("the local repository %s already has this name", shortPath(local.Path))
		}
	}
	for _, repo := range inv.remotes {
		if strings.EqualFold(repo.Name, newName) {
			return fmt.Errorf("the remote repository %s already has this name", repo.FullName())
		}
	}
	target := filepath.Join(filepath.Dir(pair.local.Path), newName)
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("%s already exists", shortPath(target))
	}
	return nil
}

type renameCandidate struct {
	name     string
	activity time.Time
}

func renameCandidates(ctx context.Context, inv inventory) []renameCandidate {
	localCount, remoteCount := nameCounts(inv)
	var out []renameCandidate
	for _, pair := range inv.pairs {
		base := filepath.Base(pair.local.Path)
		key := strings.ToLower(base)
		if base == pair.remote.Name && localCount[key] == 1 && remoteCount[key] == 1 {
			out = append(out, renameCandidate{name: base, activity: gitcmd.LastActivity(ctx, pair.local.Path)})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if !out[i].activity.Equal(out[j].activity) {
			return out[i].activity.After(out[j].activity)
		}
		return out[i].name < out[j].name
	})
	return out[:min(len(out), maxRenameCandidates)]
}

func nameCounts(inv inventory) (map[string]int, map[string]int) {
	localCount := collections.CountBy(inv.locals, func(local scan.Repo) string {
		return strings.ToLower(filepath.Base(local.Path))
	})
	remoteCount := collections.CountBy(inv.remotes, func(repo provider.Repo) string {
		return strings.ToLower(repo.Name)
	})
	return localCount, remoteCount
}

func candidateName(c renameCandidate) string {
	return c.name
}

func pickRename(candidates []renameCandidate, inv inventory) (oldName, newName string, cancelled bool, err error) {
	names := collections.Map(candidates, candidateName)
	p := terminal.NewPicker(names, func(oldName, newName string) error {
		pair, err := findRenamePair(inv, oldName)
		if err != nil {
			return err
		}
		return checkNewName(inv, pair, newName)
	})
	p.SelectPrompt = "Repository: "
	p.InputPrompt = "New name: "
	p.TitlePrefix = "Renaming "
	restore, err := terminal.MakeRaw(os.Stdin)
	if err != nil {
		return "", "", false, err
	}
	defer restore()
	return p.Run(terminal.StdinKeys(os.Stdin), terminal.NewScreen(os.Stdout))
}

func remoteProtocol(originURL, fallback string) string {
	if originURL != "" {
		return protocolOf(originURL)
	}

	return fallback
}

func protocolOf(originURL string) string {
	if strings.HasPrefix(originURL, "https://") || strings.HasPrefix(originURL, "http://") {
		return provider.ProtocolHTTPS
	}
	return provider.ProtocolSSH
}

func (s *session) executeRename(pair matched, newName string) error {
	dir := pair.local.Path
	oldName := pair.remote.Name
	originURL, err := gitcmd.RemoteURL(s.ctx, dir, pair.gitRemote())
	if err != nil {
		return err
	}
	protocol := remoteProtocol(originURL, s.cfg.Protocol)

	renamed, err := s.prov.RenameRepo(s.ctx, pair.remote.Owner, pair.remote.Name, newName)
	if err != nil {
		return err
	}
	if err := gitcmd.SetRemote(s.ctx, dir, pair.gitRemote(), s.prov.RemoteURL(renamed, protocol)); err != nil {
		return fmt.Errorf("%s renamed to %s, but %s was not updated: %w", pair.remote.FullName(), renamed.FullName(), pair.gitRemote(), err)
	}
	target := filepath.Join(filepath.Dir(dir), newName)
	if err := os.Rename(dir, target); err != nil {
		return fmt.Errorf("%s renamed to %s and %s updated, but the directory was not renamed: %w", pair.remote.FullName(), renamed.FullName(), pair.gitRemote(), err)
	}
	s.printer.Line(render.Grey, "  remote: %s → %s", pair.remote.FullName(), renamed.FullName())
	s.printer.Line(render.Grey, "  local:  %s → %s", shortPath(dir), shortPath(target))
	s.printer.Line("", "Renamed %s → %s", oldName, newName)
	return nil
}
