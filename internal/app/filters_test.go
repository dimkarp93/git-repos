package app

import "testing"

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
		if got := tc.f.matchResult(Result{Status: tc.status}); got != tc.want {
			t.Errorf("%s: matchResult(%s) = %v", tc.name, tc.status, got)
		}
	}
}

func TestFiltersMatchStatus(t *testing.T) {
	feature := statusResult{feature: true}
	dirty := statusResult{dirty: true}
	both := statusResult{feature: true, dirty: true}
	clean := statusResult{}

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
		{"wip требует обе метки", filters{wip: true}, both, true},
		{"wip не ловит одну", filters{wip: true}, feature, false},
		{"wip не ловит другую", filters{wip: true}, dirty, false},
		{"wip или feature", filters{wip: true, feature: true}, feature, true},
		{"hotfix ловит грязь на дефолтной", filters{hotfix: true}, dirty, true},
		{"hotfix не ловит фичу с грязью", filters{hotfix: true}, both, false},
		{"hotfix не ловит чистую дефолтную", filters{hotfix: true}, clean, false},
		{"pushable ловит чистую фичу", filters{pushable: true}, feature, true},
		{"pushable не ловит фичу с грязью", filters{pushable: true}, both, false},
		{"pushable не ловит чистую дефолтную", filters{pushable: true}, clean, false},
		{"hotfix или pushable", filters{hotfix: true, pushable: true}, feature, true},
		{"view-фильтр не пропускает статусы", filters{behind: true}, both, false},
	}
	for _, tc := range cases {
		if got := tc.f.matchStatus(tc.res); got != tc.want {
			t.Errorf("%s: matchStatus = %v", tc.name, got)
		}
	}
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
	out := filterReport(report, filters{behind: true})
	if len(out.LocalOnly) != 0 || len(out.RemoteOnly) != 0 {
		t.Fatalf("presence = %+v / %+v", out.LocalOnly, out.RemoteOnly)
	}
	if len(out.Repos) != 1 || out.Repos[0].FullName != "o/behind" {
		t.Fatalf("repos = %+v", out.Repos)
	}

	out = filterReport(report, filters{local: true, ahead: true})
	if len(out.LocalOnly) != 1 || len(out.RemoteOnly) != 0 {
		t.Fatalf("presence = %+v / %+v", out.LocalOnly, out.RemoteOnly)
	}
	if len(out.Repos) != 1 || out.Repos[0].FullName != "o/ahead" {
		t.Fatalf("repos = %+v", out.Repos)
	}

	if got := filterReport(report, filters{}); len(got.Repos) != 3 || len(got.LocalOnly) != 1 {
		t.Fatalf("без фильтров отчёт изменился: %+v", got)
	}
}

func TestFiltersNames(t *testing.T) {
	if got := (filters{behind: true, wip: true}).names(); got != "--behind, --wip" {
		t.Fatalf("names = %q", got)
	}
}
