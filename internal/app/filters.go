package app

import (
	"flag"
	"strings"
)

type filters struct {
	local     bool
	remote    bool
	behind    bool
	ahead     bool
	synced    bool
	conflict  bool
	failed    bool
	feature   bool
	inDevelop bool
	wip       bool
	hotfix    bool
	pushable  bool
}

var filterFlags = map[string]bool{
	"local": true, "remote": true, "behind": true, "ahead": true, "synced": true,
	"conflict": true, "failed": true, "feature": true, "in-develop": true, "wip": true,
	"hotfix": true, "pushable": true,
}

func isFilterFlag(name string) bool { return filterFlags[name] }

func (f *filters) registerView(fs *flag.FlagSet) {
	fs.BoolVar(&f.local, "local", false, "keep repositories that exist only locally")
	fs.BoolVar(&f.remote, "remote", false, "keep repositories that exist only on the provider")
	fs.BoolVar(&f.behind, "behind", false, "keep repositories whose default branch is behind")
	fs.BoolVar(&f.ahead, "ahead", false, "keep repositories whose default branch is ahead")
	fs.BoolVar(&f.synced, "synced", false, "keep repositories that are in sync")
	fs.BoolVar(&f.conflict, "conflict", false, "keep repositories whose branches diverged")
	fs.BoolVar(&f.failed, "failed", false, "keep repositories that could not be compared")
}

func (f *filters) registerStatus(fs *flag.FlagSet) {
	fs.BoolVar(&f.feature, "feature", false, "keep repositories that are not on the default branch")
	fs.BoolVar(&f.inDevelop, "in-develop", false, "keep repositories with uncommitted or untracked files")
	fs.BoolVar(&f.wip, "wip", false, "keep repositories that are both [feature] and [in develop]")
	fs.BoolVar(&f.hotfix, "hotfix", false, "keep repositories with changes on the default branch")
	fs.BoolVar(&f.pushable, "pushable", false, "keep repositories on a feature branch with a clean tree")
}

func (f *filters) registerAll(fs *flag.FlagSet) {
	f.registerView(fs)
	f.registerStatus(fs)
}

func (f filters) anyView() bool {
	return f.local || f.remote || f.behind || f.ahead || f.synced || f.conflict || f.failed
}

func (f filters) anyStatus() bool {
	return f.feature || f.inDevelop || f.wip || f.hotfix || f.pushable
}

func (f filters) any() bool { return f.anyView() || f.anyStatus() }

func (f filters) names() string {
	pairs := []struct {
		on   bool
		name string
	}{
		{f.local, "--local"},
		{f.remote, "--remote"},
		{f.behind, "--behind"},
		{f.ahead, "--ahead"},
		{f.synced, "--synced"},
		{f.conflict, "--conflict"},
		{f.failed, "--failed"},
		{f.feature, "--feature"},
		{f.inDevelop, "--in-develop"},
		{f.wip, "--wip"},
		{f.hotfix, "--hotfix"},
		{f.pushable, "--pushable"},
	}
	out := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if pair.on {
			out = append(out, pair.name)
		}
	}
	return strings.Join(out, ", ")
}

func (f filters) matchLocalOnly() bool {
	if !f.any() {
		return true
	}
	return f.local
}

func (f filters) matchRemoteOnly() bool {
	if !f.any() {
		return true
	}
	return f.remote
}

func (f filters) matchResult(res Result) bool {
	if !f.any() {
		return true
	}
	switch res.Status {
	case StatusPull:
		return f.behind
	case StatusPush:
		return f.ahead
	case StatusSynced:
		return f.synced
	case StatusConflict:
		return f.conflict
	case StatusError, StatusUnknown, StatusNoBranch:
		return f.failed
	default:
		return false
	}
}

func (f filters) matchStatus(res statusResult) bool {
	if !f.any() {
		return true
	}
	if f.wip && res.feature && res.dirty {
		return true
	}
	if f.hotfix && !res.feature && res.dirty {
		return true
	}
	if f.pushable && res.feature && !res.dirty {
		return true
	}
	if f.feature && res.feature {
		return true
	}
	return f.inDevelop && res.dirty
}
