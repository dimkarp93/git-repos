package app

import (
	"flag"
	"fmt"
	"os"

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

	candidates := make([]LocalOnly, 0, len(inv.localOnly))
	for _, item := range inv.localOnly {
		if item.Orphan() {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		s.printer.Line(render.Grey, "Локальных репозиториев без удалённого нет.")
		return ExitOK
	}

	if opts.dryRun {
		for _, item := range candidates {
			s.printer.Line(render.Orange, "  %s (%s)", shortPath(item.Path), item.Reason)
		}
		s.printer.Line("", "Кандидатов на удаление: %d", len(candidates))
		return ExitOK
	}

	c := newConfirmer(opts.assumeYes)
	deleted, failed := s.cleanLocal(candidates, c)
	printCleanSummary(s, deleted, failed)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func (s *session) cleanLocal(candidates []LocalOnly, c *confirmer) (deleted, failed int) {
	for _, item := range candidates {
		prompt := fmt.Sprintf("Удалить локальный репозиторий %s?\n  причина: %s%s",
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
			s.printer.Line(render.Grey, "Остановлено пользователем.")
			return deleted, failed
		}
		if err := os.RemoveAll(item.Path); err != nil {
			failed++
			s.printer.Line(render.Red, "  ошибка: %v", err)
			continue
		}
		deleted++
		s.printer.Line(render.Grey, "  удалено: %s", shortPath(item.Path))
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
		s.printer.Line(render.Grey, "Удалённых репозиториев без локального нет.")
		return ExitOK
	}

	if opts.dryRun {
		for _, item := range inv.remoteOnly {
			s.printer.Line(render.Yellow, "  %s", item.FullName)
		}
		s.printer.Line("", "Кандидатов на удаление: %d", len(inv.remoteOnly))
		return ExitOK
	}

	c := newConfirmer(opts.assumeYes)
	deleted, failed := s.cleanRemote(inv.remoteOnly, c)
	printCleanSummary(s, deleted, failed)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func (s *session) cleanRemote(candidates []RemoteOnly, c *confirmer) (deleted, failed int) {
	for _, item := range candidates {
		note := ""
		if item.Archived {
			note = "\n  репозиторий архивный"
		}
		if item.repo.Private {
			note += "\n  репозиторий приватный"
		}
		prompt := fmt.Sprintf("Удалить %s %s?%s\n  %s", s.prov.Name(), item.FullName, note, item.WebURL)
		ans, err := c.ask(prompt)
		if err != nil {
			s.printer.Line(render.Red, "  %v", err)
			return deleted, failed + 1
		}
		switch ans {
		case answerSkip:
			continue
		case answerQuit:
			s.printer.Line(render.Grey, "Остановлено пользователем.")
			return deleted, failed
		}
		if err := s.prov.DeleteRepo(s.ctx, item.repo.Owner, item.repo.Name); err != nil {
			failed++
			s.printer.Line(render.Red, "  ошибка: %v", err)
			if s.ctx.Err() != nil {
				return deleted, failed
			}
			continue
		}
		deleted++
		s.printer.Line(render.Grey, "  удалено: %s", item.FullName)
	}
	return deleted, failed
}

func dirtyNote(s *session, path string) string {
	dirty, err := gitcmd.IsDirty(s.ctx, path)
	if err != nil || !dirty {
		return ""
	}
	return "\n  внимание: есть незакоммиченные изменения"
}

func printCleanSummary(s *session, deleted, failed int) {
	s.printer.Line("", "Итог: удалено — %d · ошибок — %d", deleted, failed)
}
