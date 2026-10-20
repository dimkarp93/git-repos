package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFiltersMatchResult(t *testing.T) {
	cases := []struct {
		name   string
		f      filters
		status Status
		want   bool
	}{
		{"без фильтров всё проходит", filters{}, StatusConflict, true},
		{"behind ловит pull", filters{behind: true}, StatusPull, true},
		{"behind не ловит push", filters{behind: true}, StatusPush, false},
		{"или из двух", filters{behind: true, ahead: true}, StatusPush, true},
		{"synced", filters{synced: true}, StatusSynced, true},
		{"conflict", filters{conflict: true}, StatusConflict, true},
		{"failed ловит error", filters{failed: true}, StatusError, true},
		{"failed ловит unknown", filters{failed: true}, StatusUnknown, true},
		{"failed ловит no-branch", filters{failed: true}, StatusNoBranch, true},
		{"status-фильтр не пропускает ветки", filters{feature: true}, StatusPull, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.f.matchResult(Result{Status: tc.status}))
		})
	}
}

func TestFiltersMatchStatus(t *testing.T) {
	feature := statusResult{feature: true}
	dirty := statusResult{dirty: true}
	both := statusResult{feature: true, dirty: true}
	clean := statusResult{}
	real := statusResult{feature: true, real: true}

	cases := []struct {
		name string
		f    filters
		res  statusResult
		want bool
	}{
		{"без фильтров", filters{}, clean, true},
		{"feature", filters{feature: true}, feature, true},
		{"feature не ловит dirty", filters{feature: true}, dirty, false},
		{"in-develop", filters{inDevelop: true}, dirty, true},
		{"или из двух", filters{feature: true, inDevelop: true}, dirty, true},
		{"wip ловит обе метки", filters{wip: true}, both, true},
		{"wip ловит одну feature", filters{wip: true}, feature, true},
		{"wip ловит одну dirty", filters{wip: true}, dirty, true},
		{"wip не ловит чистую дефолтную", filters{wip: true}, clean, false},
		{"wip или feature", filters{wip: true, feature: true}, feature, true},
		{"hotfix ловит грязь на дефолтной", filters{hotfix: true}, dirty, true},
		{"hotfix не ловит фичу с грязью", filters{hotfix: true}, both, false},
		{"hotfix не ловит чистую дефолтную", filters{hotfix: true}, clean, false},
		{"pushable ловит чистую фичу", filters{pushable: true}, feature, true},
		{"pushable не ловит фичу с грязью", filters{pushable: true}, both, false},
		{"pushable не ловит чистую дефолтную", filters{pushable: true}, clean, false},
		{"hotfix или pushable", filters{hotfix: true, pushable: true}, feature, true},
		{"view-фильтр не пропускает статусы", filters{behind: true}, both, false},
		{"workspace — feature", filters{feature: true}, real, true},
		{"workspace — wip", filters{wip: true}, real, true},
		{"workspace не pushable", filters{pushable: true}, real, false},
		{"workspace не in-develop", filters{inDevelop: true}, real, false},
		{"workspace не hotfix", filters{hotfix: true}, real, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.f.matchStatus(tc.res))
	}
}

func TestFiltersMatchName(t *testing.T) {
	repo := Result{FullName: "acme/git-repos", Branch: "main"}
	cases := []struct {
		name string
		f    filters
		want bool
	}{
		{"без фильтра имени", filters{}, true},
		{"project совпадает по подстроке", filters{project: "repos"}, true},
		{"project не совпадает", filters{project: "other"}, false},
		{"project не ловит owner", filters{project: "acme"}, false},
		{"project без учёта регистра", filters{project: "REPOS"}, true},
		{"branch совпадает по подстроке", filters{branch: "mai"}, true},
		{"branch не совпадает", filters{branch: "dev"}, false},
		{"project и branch вместе", filters{project: "repos", branch: "main"}, true},
		{"project и branch — один не совпал", filters{project: "repos", branch: "dev"}, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.f.matchResult(repo))
	}
}

func TestFiltersMatchNameStatus(t *testing.T) {
	res := statusResult{name: "git-repos", branch: "feature/x"}
	cases := []struct {
		name string
		f    filters
		want bool
	}{
		{"project совпадает", filters{project: "repos"}, true},
		{"project не совпадает", filters{project: "other"}, false},
		{"branch совпадает по подстроке", filters{branch: "feature"}, true},
		{"branch не совпадает", filters{branch: "main"}, false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.f.matchStatus(res))
	}
}

type reportView struct {
	local, remote int
	repos         []string
}

func viewReport(r Report) reportView {
	v := reportView{local: len(r.LocalOnly), remote: len(r.RemoteOnly)}
	for _, res := range r.Repos {
		v.repos = append(v.repos, res.FullName)
	}
	return v
}

func TestFilterReportKeepsOnlySelected(t *testing.T) {
	report := Report{
		LocalOnly:  []LocalOnly{{Path: "/a"}},
		RemoteOnly: []RemoteOnly{{FullName: "o/b"}},
		Repos: []Result{
			{FullName: "o/behind", Status: StatusPull},
			{FullName: "o/ahead", Status: StatusPush},
			{FullName: "o/synced", Status: StatusSynced},
		},
	}
	cases := map[string]struct {
		f    filters
		want reportView
	}{
		"behind":         {f: filters{behind: true}, want: reportView{repos: []string{"o/behind"}}},
		"local or ahead": {f: filters{local: true, ahead: true}, want: reportView{local: 1, repos: []string{"o/ahead"}}},
		"no filters":     {f: filters{}, want: reportView{local: 1, remote: 1, repos: []string{"o/behind", "o/ahead", "o/synced"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, viewReport(filterReport(report, tc.f)))
		})
	}
}

func TestFiltersNames(t *testing.T) {
	cases := map[string]filters{
		"--behind, --wip":                         {behind: true, wip: true},
		"--behind, --project=acme, --branch=main": {behind: true, project: "acme", branch: "main"},
	}
	for want, f := range cases {
		t.Run(want, func(t *testing.T) {
			assert.Equal(t, want, f.names())
		})
	}
}
