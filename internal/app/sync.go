package app

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/dimkarp93/git-repos/internal/config"
	"github.com/dimkarp93/git-repos/internal/gitcmd"
	"github.com/dimkarp93/git-repos/internal/provider"
	"github.com/dimkarp93/git-repos/internal/render"
)

type syncAction struct {
	Target string
	Detail string
	URL    string
	Err    error
	Done   bool
	Plan   bool
}

func runSync(args []string) int {
	opts := options{}
	fs := newFlagSet("sync", &opts)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "show what would be done and change nothing")
	fs.StringVar(&opts.into, "into", "", "directory for cloned repositories (required)")
	fs.StringVar(&opts.protocol, "protocol", "", "remote protocol for new remotes and clones: ssh or https")
	fs.BoolVar(&opts.noProgress, "no-progress", false, "do not show the progress indicator")
	if ok, code := parseFlags(fs, args); !ok {
		return code
	}
	if opts.protocol != "" && opts.protocol != provider.ProtocolSSH && opts.protocol != provider.ProtocolHTTPS {
		return fail(errors.New("--protocol принимает ssh или https"))
	}
	if opts.into == "" {
		return fail(errors.New("нужен --into <dir>: каталог, куда клонировать удалённые репозитории"))
	}
	into := config.ExpandPath(opts.into)
	if info, err := os.Stat(into); err != nil || !info.IsDir() {
		return fail(errors.New("каталог для клонирования недоступен: " + into))
	}

	s, err := start(opts)
	if err != nil {
		return fail(err)
	}
	defer s.close()

	s.pr.setPlan(6)
	s.spinner.Start(s.pr.label)
	pairs, inv, err := collect(s.ctx, s.prov, s.store, s.cfg, s.roots, opts, s.pr)
	if err != nil {
		return fail(err)
	}

	pushed := s.pushMissing(inv, opts)
	filled := s.fillEmpty(pairs, opts)
	cloned := s.cloneMissing(inv, into, opts)
	s.spinner.Stop()

	report(s, "СОЗДАНО НА "+upperName(s.prov.Name()), pushed)
	report(s, "ЗАПУШЕНО В ПУСТЫЕ НА "+upperName(s.prov.Name()), filled)
	report(s, "КЛОНИРОВАНО В "+render.Ellipsis(shortPath(into), maxPathWidth), cloned)

	links := make([]string, 0, len(pushed))
	for _, action := range append(append([]syncAction{}, pushed...), filled...) {
		if action.Done && action.URL != "" {
			links = append(links, action.URL)
		}
	}
	if len(links) > 0 {
		s.printer.Line(render.Bold, "Ссылки на созданные репозитории:")
		for _, link := range links {
			s.printer.Line("", "  %s", link)
		}
		s.printer.Line("", "")
	}

	failed := countFailed(pushed) + countFailed(filled) + countFailed(cloned)
	s.printer.Line("", "Итог: создано — %d · запушено в пустые — %d · клонировано — %d · пропущено с ошибкой — %d",
		countDone(pushed), countDone(filled), countDone(cloned), failed)
	if failed > 0 {
		return ExitFailure
	}
	return ExitOK
}

func (s *session) pushMissing(inv inventory, opts options) []syncAction {
	actions := make([]syncAction, 0, len(inv.localOnly))
	s.pr.setPhase("создание на " + s.prov.Name())
	s.pr.setUnit("репозиториев")
	s.pr.setTotal(countOrphans(inv))
	for _, item := range inv.localOnly {
		if !item.Orphan() {
			continue
		}
		s.pr.step(item.Name, len(actions))
		action := syncAction{Target: shortPath(item.Path), Detail: item.Name}
		switch {
		case item.Owner != "" && item.Owner != inv.account:
			action.Detail = item.Owner + "/" + item.Name + ": чужой владелец"
			actions = append(actions, action)
			continue
		case nameTaken(inv, inv.account, item.Name):
			action.Detail = item.Name + ": имя занято, конфликт"
			actions = append(actions, action)
			continue
		}
		if opts.dryRun {
			action.Detail = "будет создан приватный " + inv.account + "/" + item.Name
			action.Plan = true
			actions = append(actions, action)
			continue
		}

		created, err := s.prov.CreateRepo(s.ctx, item.Name, true)
		if err != nil {
			action.Err = err
			actions = append(actions, action)
			continue
		}
		action.Done = true
		action.URL = created.WebURL
		action.Detail = created.FullName()

		remoteURL := s.prov.RemoteURL(created, s.cfg.Protocol)
		if err := gitcmd.SetRemote(s.ctx, item.Path, "origin", remoteURL); err != nil {
			action.Err = err
			actions = append(actions, action)
			continue
		}
		if !gitcmd.HasCommits(s.ctx, item.Path) {
			action.Detail += " (пустой, push пропущен)"
			actions = append(actions, action)
			continue
		}
		branch, err := gitcmd.CurrentBranch(s.ctx, item.Path)
		if err != nil {
			action.Err = err
			actions = append(actions, action)
			continue
		}
		if err := gitcmd.Push(s.ctx, item.Path, "origin", branch); err != nil {
			action.Err = err
			actions = append(actions, action)
			continue
		}
		action.Detail += " ← " + branch
		actions = append(actions, action)
	}
	s.pr.complete()
	return actions
}

func (s *session) fillEmpty(pairs []matched, opts options) []syncAction {
	s.pr.setPhase("поиск пустых на " + s.prov.Name())
	s.pr.setUnit("репозиториев")
	s.pr.setTotal(len(pairs))
	actions := make([]syncAction, 0)
	for i, pair := range pairs {
		s.pr.step(pair.remote.FullName(), i)
		if !gitcmd.HasCommits(s.ctx, pair.local.Path) {
			continue
		}
		if gitcmd.HasRemoteRefs(s.ctx, pair.local.Path, "origin") {
			continue
		}
		branch := resolveBranch(s.store, s.prov.Name(), pair.remote, opts.refresh)
		if branch == "" {
			continue
		}
		if _, err := s.prov.BranchHead(s.ctx, pair.remote.Owner, pair.remote.Name, branch); !errors.Is(err, provider.ErrNotFound) {
			continue
		}

		action := syncAction{Target: shortPath(pair.local.Path), Detail: pair.remote.FullName() + ": удалённый пуст", URL: pair.remote.WebURL}
		if opts.dryRun {
			action.Detail = "будет запушен в пустой " + pair.remote.FullName()
			action.Plan = true
			actions = append(actions, action)
			continue
		}
		local, err := gitcmd.CurrentBranch(s.ctx, pair.local.Path)
		if err != nil {
			action.Err = err
			actions = append(actions, action)
			continue
		}
		if err := gitcmd.Push(s.ctx, pair.local.Path, "origin", local); err != nil {
			action.Err = err
			actions = append(actions, action)
			continue
		}
		action.Done = true
		action.Detail = pair.remote.FullName() + " ← " + local
		actions = append(actions, action)
	}
	s.pr.complete()
	return actions
}

func (s *session) cloneMissing(inv inventory, into string, opts options) []syncAction {
	actions := make([]syncAction, 0, len(inv.remoteOnly))
	s.pr.setPhase("клонирование в " + shortPath(into))
	s.pr.setUnit("репозиториев")
	s.pr.setTotal(len(inv.remoteOnly))
	for _, item := range inv.remoteOnly {
		s.pr.step(item.FullName, len(actions))
		dest := filepath.Join(into, item.repo.Name)
		action := syncAction{Target: item.FullName, Detail: shortPath(dest), URL: item.WebURL}
		if _, err := os.Stat(dest); err == nil {
			action.Detail = shortPath(dest) + ": каталог занят, конфликт"
			actions = append(actions, action)
			continue
		}
		if opts.dryRun {
			action.Detail = "будет склонирован в " + shortPath(dest)
			action.Plan = true
			actions = append(actions, action)
			continue
		}
		if err := gitcmd.Clone(s.ctx, into, s.prov.RemoteURL(item.repo, s.cfg.Protocol), item.repo.Name, item.repo.DefaultBranch); err != nil {
			action.Err = err
			actions = append(actions, action)
			continue
		}
		action.Done = true
		actions = append(actions, action)
	}
	s.pr.complete()
	return actions
}

func countOrphans(inv inventory) int {
	n := 0
	for _, item := range inv.localOnly {
		if item.Orphan() {
			n++
		}
	}
	return n
}

func nameTaken(inv inventory, account, name string) bool {
	for _, repo := range inv.remotes {
		if repo.Owner == account && repo.Name == name {
			return true
		}
	}
	return false
}

func report(s *session, title string, actions []syncAction) {
	if len(actions) == 0 {
		return
	}
	rows := make([][]render.Cell, 0, len(actions))
	for _, action := range actions {
		status := render.Cell{Text: "пропущено", Color: render.Yellow}
		switch {
		case action.Err != nil:
			status = render.Cell{Text: "ошибка: " + errText(action.Err), Color: render.Red}
		case action.Done:
			status = render.Cell{Text: "готово", Color: render.Green}
		case action.Plan:
			status = render.Cell{Text: "план", Color: render.Grey}
		}
		rows = append(rows, []render.Cell{
			render.Plain(render.Ellipsis(action.Target, maxPathWidth)),
			{Text: render.Ellipsis(action.Detail, maxDetailWidth), Color: render.Grey},
			status,
		})
	}
	s.printer.Table(title, []string{"ИСТОЧНИК", "НАЗНАЧЕНИЕ", "СТАТУС"}, rows)
	s.printer.Line("", "")
}

func countDone(actions []syncAction) int {
	n := 0
	for _, action := range actions {
		if action.Done {
			n++
		}
	}
	return n
}

func countFailed(actions []syncAction) int {
	n := 0
	for _, action := range actions {
		if action.Err != nil {
			n++
		}
	}
	return n
}

func upperName(s string) string {
	switch s {
	case "github":
		return "GITHUB"
	default:
		return s
	}
}
