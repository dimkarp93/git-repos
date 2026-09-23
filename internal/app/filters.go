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
	branch    string
	project   string
}

var filterFlags = map[string]bool{
	"local": true, "remote": true, "behind": true, "ahead": true, "synced": true,
	"conflict": true, "failed": true, "feature": true, "in-develop": true, "wip": true,
	"hotfix": true, "pushable": true, "branch": true, "project": true,
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
	fs.BoolVar(&f.wip, "wip", false, "keep repositories that are [feature] or [in-develop]")
	fs.BoolVar(&f.hotfix, "hotfix", false, "keep repositories with changes on the default branch")
	fs.BoolVar(&f.pushable, "pushable", false, "keep repositories on a feature branch with a clean tree")
}

func (f *filters) registerName(fs *flag.FlagSet) {
	fs.StringVar(&f.branch, "branch", "", "keep repositories whose branch contains this substring (case-insensitive)")
	fs.StringVar(&f.project, "project", "", "keep repositories whose name contains this substring (case-insensitive)")
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

func (f filters) anyName() bool { return f.branch != "" || f.project != "" }

func (f filters) any() bool { return f.anyView() || f.anyStatus() || f.anyName() }

func (f filters) matchName(name, branch string) bool {
	if f.project != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(f.project)) {
		return false
	}
	if f.branch != "" && !strings.Contains(strings.ToLower(branch), strings.ToLower(f.branch)) {
		return false
	}
	return true
}

func repoName(fullName string) string {
	if idx := strings.LastIndex(fullName, "/"); idx >= 0 {
		return fullName[idx+1:]
	}
	return fullName
}

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
	out := make([]string, 0, len(pairs)+2)
	for _, pair := range pairs {
		if pair.on {
			out = append(out, pair.name)
		}
	}
	if f.project != "" {
		out = append(out, "--project="+f.project)
	}
	if f.branch != "" {
		out = append(out, "--branch="+f.branch)
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
	if !f.matchName(repoName(res.FullName), res.Branch) {
		return false
	}
	if !f.anyView() && !f.anyStatus() {
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
	if !f.matchName(res.name, res.branch) {
		return false
	}
	if !f.anyStatus() && !f.anyView() {
		return true
	}
	for _, m := range statusMarks() {
		if m.want(f) && m.on(res) {
			return true
		}
	}
	return false
}
