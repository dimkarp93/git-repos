package app

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/render"
)

func cleanLocalFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("clean-local", opts)
	fs.BoolVar(&opts.assumeYes, "yes", false, "do not ask, assume yes-to-all")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "list candidates and delete nothing")
	return fs
}

func runCleanLocal(args []string) int {
	opts := options{}
	fs := cleanLocalFlags(&opts)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	_, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		return fail(err)
	}
	return s.cleanOrphans(inv, opts)
}

func (s *session) cleanOrphans(inv inventory, opts options) int {
	candidates := orphans(inv.localOnly)
	if len(candidates) == 0 {
		s.printer.Line(render.Grey, "No local repositories without a remote.")
		return ExitOK
	}

	candidates, blocked, err := s.guardLocal(candidates)
	if err != nil {
		return fail(err)
	}

	if opts.dryRun {
		printLocalCandidates(s.printer, candidates)
		return exitSkipped(blocked)
	}

	deleted, failed := s.cleanLocal(candidates, newConfirmer(opts.assumeYes))
	printCleanSummary(s, deleted, failed)
	if failed > 0 || len(blocked) > 0 {
		return ExitFailure
	}
	return ExitOK
}

func orphans(items []LocalOnly) []LocalOnly {
	out := make([]LocalOnly, 0, len(items))
	for _, item := range items {
		if item.Orphan() {
			out = append(out, item)
		}
	}
	return out
}

func printLocalCandidates(p *render.Printer, candidates []LocalOnly) {
	for _, item := range candidates {
		p.Line(render.Orange, "  %s (%s)", shortPath(item.Path), item.Reason)
	}
	p.Line("", "Deletion candidates: %d", len(candidates))
}

func (s *session) guardLocal(candidates []LocalOnly) ([]LocalOnly, []skippedRepo, error) {
	g, err := newGuard(s.ctx, nil)
	if err != nil {
		return nil, nil, err
	}

	kept, blocked := guardCandidates(g, candidates)
	printSkipped(s.printer, blocked)
	return kept, blocked, nil
}

func guardCandidates(g *guard, candidates []LocalOnly) ([]LocalOnly, []skippedRepo) {
	var kept []LocalOnly
	var blocked []skippedRepo
	for _, item := range candidates {
		if row, skip := g.check(item.Path); skip {
			blocked = append(blocked, row)
			continue
		}
		kept = append(kept, item)
	}
	return kept, blocked
}

func exitSkipped(blocked []skippedRepo) int {
	if len(blocked) > 0 {
		return ExitFailure
	}
	return ExitOK
}

func (s *session) cleanLocal(candidates []LocalOnly, c *confirmer) (deleted, failed int) {
	for _, item := range candidates {
		prompt := fmt.Sprintf("Delete the local repository %s?\n  reason: %s%s",
			shortPath(item.Path), item.Reason, dirtyNote(s, item.Path))
		ans, err := c.ask(prompt)
		if err != nil {
			s.printer.Line(render.Red, "  %v", err)
			return deleted, failed + 1
		}
		switch ans {
		case answerSkip:
			continue
		case answerQuit:
			s.printer.Line(render.Grey, "Stopped by the user.")
			return deleted, failed
		}
		if err := os.RemoveAll(item.Path); err != nil {
			failed++
			s.printer.Line(render.Red, "  error: %v", err)
			continue
		}
		deleted++
		s.printer.Line(render.Grey, "  deleted: %s", shortPath(item.Path))
	}
	return deleted, failed
}

func cleanRemoteFlags(opts *options) *flag.FlagSet {
	fs := newFlagSet("clean-remote", opts)
	fs.BoolVar(&opts.assumeYes, "yes", false, "do not ask, assume yes-to-all")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "list candidates and delete nothing")
	return fs
}

func runCleanRemote(args []string) int {
	opts := options{}
	fs := cleanRemoteFlags(&opts)
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	_, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		return fail(err)
	}
	if len(inv.remoteOnly) == 0 {
		s.printer.Line(render.Grey, "No remote repositories without a local copy.")
		return ExitOK
	}

	if opts.dryRun {
		printRemoteCandidates(s.printer, inv.remoteOnly)
		return ExitOK
	}

	deleted, failed := s.cleanRemote(inv.remoteOnly, newConfirmer(opts.assumeYes))
	printCleanSummary(s, deleted, failed)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func printRemoteCandidates(p *render.Printer, candidates []RemoteOnly) {
	for _, item := range candidates {
		p.Line(render.Yellow, "  %s", item.FullName)
	}
	p.Line("", "Deletion candidates: %d", len(candidates))
}

func remoteNote(item RemoteOnly) string {
	var notes []string
	if item.Archived {
		notes = append(notes, "\n  the repository is archived")
	}
	if item.repo.Private {
		notes = append(notes, "\n  the repository is private")
	}
	return strings.Join(notes, "")
}

func (s *session) cleanRemote(candidates []RemoteOnly, c *confirmer) (deleted, failed int) {
	for _, item := range candidates {
		prompt := fmt.Sprintf("Delete %s %s?%s\n  %s", s.prov.Name(), item.FullName, remoteNote(item), item.WebURL)
		ans, err := c.ask(prompt)
		if err != nil {
			s.printer.Line(render.Red, "  %v", err)
			return deleted, failed + 1
		}
		switch ans {
		case answerSkip:
			continue
		case answerQuit:
			s.printer.Line(render.Grey, "Stopped by the user.")
			return deleted, failed
		}
		if err := s.prov.DeleteRepo(s.ctx, item.repo.Owner, item.repo.Name); err != nil {
			failed++
			s.printer.Line(render.Red, "  error: %v", err)
			if s.ctx.Err() != nil {
				return deleted, failed
			}
			continue
		}
		deleted++
		s.printer.Line(render.Grey, "  deleted: %s", item.FullName)
	}
	return deleted, failed
}

func dirtyNote(s *session, path string) string {
	dirty, err := gitcmd.IsDirty(s.ctx, path)
	if err != nil || !dirty {
		return ""
	}
	return "\n  warning: there are uncommitted changes"
}

func printCleanSummary(s *session, deleted, failed int) {
	s.printer.Line("", "Summary: deleted — %d · errors — %d", deleted, failed)
}
