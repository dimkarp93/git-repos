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
	"github.com/dimkarp93/git-repos/internal/terminal"
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
	opts := options{}
	fs := renameFlags(&opts)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitFailure
	}
	rest := fs.Args()
	if len(rest) != 0 && len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "git-repos: rename takes either no arguments or <old-name> <new-name>")
		return ExitFailure
	}
	interactive := len(rest) == 0
	if interactive && !(render.IsTerminal(os.Stdin) && render.IsTerminal(os.Stdout)) {
		return fail(errors.New("no terminal for the interactive mode, use: git-repos rename <old-name> <new-name>"))
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

	var pair matched
	var newName string
	if interactive {
		candidates := renameCandidates(s.ctx, inv)
		if len(candidates) == 0 {
			s.printer.Line(render.Grey, "No repositories that exist both locally and on %s.", s.prov.Name())
			return ExitOK
		}
		oldName, chosen, cancelled, err := pickRename(candidates, inv)
		if err != nil {
			return fail(err)
		}
		if cancelled {
			s.printer.Line("", "Operation cancelled")
			return ExitOK
		}
		pair, err = findRenamePair(inv, oldName)
		if err != nil {
			return fail(err)
		}
		newName = chosen
	} else {
		pair, err = findRenamePair(inv, rest[0])
		if err != nil {
			return fail(err)
		}
		if err := checkNewName(inv, pair, rest[1]); err != nil {
			return fail(err)
		}
		newName = rest[1]
	}

	if err := s.executeRename(pair, newName); err != nil {
		return fail(err)
	}
	return ExitOK
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
	var locals []string
	for _, local := range inv.locals {
		if strings.EqualFold(filepath.Base(local.Path), oldName) {
			locals = append(locals, local.Path)
		}
	}
	var remotes []provider.Repo
	for _, repo := range inv.remotes {
		if strings.EqualFold(repo.Name, oldName) {
			remotes = append(remotes, repo)
		}
	}
	switch {
	case len(locals) == 0:
		return matched{}, fmt.Errorf("no local repository named %q", oldName)
	case len(locals) > 1:
		return matched{}, fmt.Errorf("%d local repositories named %q: %s", len(locals), oldName, joinShort(locals))
	case len(remotes) == 0:
		return matched{}, fmt.Errorf("no remote repository named %q", oldName)
	case len(remotes) > 1:
		names := make([]string, len(remotes))
		for i, repo := range remotes {
			names[i] = repo.FullName()
		}
		return matched{}, fmt.Errorf("%d remote repositories named %q: %s", len(remotes), oldName, strings.Join(names, ", "))
	}
	for _, pair := range inv.pairs {
		if pair.local.Path == locals[0] && strings.EqualFold(pair.remote.FullName(), remotes[0].FullName()) {
			return pair, nil
		}
	}
	return matched{}, fmt.Errorf("%s is not linked to %s through origin", shortPath(locals[0]), remotes[0].FullName())
}

func joinShort(paths []string) string {
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = shortPath(path)
	}
	return strings.Join(out, ", ")
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
	localCount := map[string]int{}
	for _, local := range inv.locals {
		localCount[strings.ToLower(filepath.Base(local.Path))]++
	}
	remoteCount := map[string]int{}
	for _, repo := range inv.remotes {
		remoteCount[strings.ToLower(repo.Name)]++
	}
	var out []renameCandidate
	for _, pair := range inv.pairs {
		base := filepath.Base(pair.local.Path)
		key := strings.ToLower(base)
		if base != pair.remote.Name || localCount[key] != 1 || remoteCount[key] != 1 {
			continue
		}
		out = append(out, renameCandidate{name: base, activity: gitcmd.LastActivity(ctx, pair.local.Path)})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].activity.Equal(out[j].activity) {
			return out[i].activity.After(out[j].activity)
		}
		return out[i].name < out[j].name
	})
	if len(out) > maxRenameCandidates {
		out = out[:maxRenameCandidates]
	}
	return out
}

func pickRename(candidates []renameCandidate, inv inventory) (oldName, newName string, cancelled bool, err error) {
	names := make([]string, len(candidates))
	for i, c := range candidates {
		names[i] = c.name
	}
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

func protocolOf(originURL string) string {
	if strings.HasPrefix(originURL, "https://") || strings.HasPrefix(originURL, "http://") {
		return provider.ProtocolHTTPS
	}
	return provider.ProtocolSSH
}

func (s *session) executeRename(pair matched, newName string) error {
	dir := pair.local.Path
	oldName := pair.remote.Name
	originURL, err := gitcmd.OriginURL(s.ctx, dir)
	if err != nil {
		return err
	}
	protocol := s.cfg.Protocol
	if originURL != "" {
		protocol = protocolOf(originURL)
	}

	renamed, err := s.prov.RenameRepo(s.ctx, pair.remote.Owner, pair.remote.Name, newName)
	if err != nil {
		return err
	}
	if err := gitcmd.SetRemote(s.ctx, dir, "origin", s.prov.RemoteURL(renamed, protocol)); err != nil {
		return fmt.Errorf("%s renamed to %s, but origin was not updated: %w", pair.remote.FullName(), renamed.FullName(), err)
	}
	target := filepath.Join(filepath.Dir(dir), newName)
	if err := os.Rename(dir, target); err != nil {
		return fmt.Errorf("%s renamed to %s and origin updated, but the directory was not renamed: %w", pair.remote.FullName(), renamed.FullName(), err)
	}
	s.printer.Line(render.Grey, "  remote: %s → %s", pair.remote.FullName(), renamed.FullName())
	s.printer.Line(render.Grey, "  local:  %s → %s", shortPath(dir), shortPath(target))
	s.printer.Line("", "Renamed %s → %s", oldName, newName)
	return nil
}
