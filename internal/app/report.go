package app

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/dimkarp93/git-repos/internal/render"
)

const (
	maxPathWidth   = 96
	maxDetailWidth = 96
)

func homeDir() (string, error) {
	return os.UserHomeDir()
}

func writeJSON(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func statusCell(res Result) render.Cell {
	switch res.Status {
	case StatusSynced:
		return render.Cell{Text: "synced", Color: render.Grey}
	case StatusPull:
		return render.Cell{Text: "behind → pull", Color: render.Orange}
	case StatusPush:
		return render.Cell{Text: "ahead → push", Color: render.Yellow}
	case StatusConflict:
		return render.Cell{Text: "conflict", Color: render.Red}
	case StatusNoBranch:
		return render.Cell{Text: "no local branch", Color: render.Grey}
	case StatusError:
		return render.Cell{Text: "error", Color: render.Red}
	default:
		return render.Cell{Text: "unknown", Color: render.Grey}
	}
}

func printReport(p *render.Printer, report Report, showAll bool, f filters) {
	p.Line(render.Bold, "%s: %s — %d local, %d remote", report.Provider, report.Account, report.LocalCount, report.RemoteCount)
	fmt.Fprintln(p.Out)

	shown := filterReport(report, f)
	printPresence(p, shown, f)
	printMismatched(p, shown)
	printBranches(p, shown, showAll || f.synced)
	printSummary(p, shown, f)
}

func filterReport(report Report, f filters) Report {
	if !f.any() {
		return report
	}
	out := report
	out.LocalOnly = nil
	out.RemoteOnly = nil
	out.Repos = nil
	if f.matchLocalOnly() {
		out.LocalOnly = report.LocalOnly
	}
	if f.matchRemoteOnly() {
		out.RemoteOnly = report.RemoteOnly
	}
	for _, res := range report.Repos {
		if f.matchResult(res) {
			out.Repos = append(out.Repos, res)
		}
	}
	if !f.matchLocalOnly() && !f.matchRemoteOnly() {
		out.Mismatched = nil
	}
	return out
}

func printPresence(p *render.Printer, report Report, f filters) {
	if len(report.LocalOnly) == 0 && len(report.RemoteOnly) == 0 {
		if f.any() && !f.local && !f.remote {
			return
		}
		p.Line(render.Grey, "The repository sets match.")
		fmt.Fprintln(p.Out)
		return
	}

	n := max(len(report.LocalOnly), len(report.RemoteOnly))
	rows := make([][]render.Cell, 0, n)
	for i := range n {
		rows = append(rows, []render.Cell{localOnlyCell(report.LocalOnly, i), remoteOnlyCell(report.RemoteOnly, i)})
	}
	p.Table("Repository sets", []string{"LOCAL ONLY", "REMOTE ONLY"}, rows)
	fmt.Fprintln(p.Out)
}

func localOnlyCell(items []LocalOnly, i int) render.Cell {
	if i >= len(items) {
		return render.Cell{}
	}

	text := render.Ellipsis(shortPath(items[i].Path), maxPathWidth)
	if items[i].Reason != "" {
		text += " (" + items[i].Reason + ")"
	}
	return render.Cell{Text: text, Color: render.Orange}
}

func remoteOnlyCell(items []RemoteOnly, i int) render.Cell {
	if i >= len(items) {
		return render.Cell{}
	}

	text := items[i].FullName
	if items[i].Archived {
		text += " (archived)"
	}
	return render.Cell{Text: text, Color: render.Yellow}
}

func printMismatched(p *render.Printer, report Report) {
	if len(report.Mismatched) == 0 {
		return
	}
	rows := make([][]render.Cell, 0, len(report.Mismatched))
	for _, item := range report.Mismatched {
		rows = append(rows, []render.Cell{
			render.Plain(render.Ellipsis(shortPath(item.Path), maxPathWidth)),
			{Text: item.Origin, Color: render.Yellow},
			{Text: item.Canonical, Color: render.Grey},
		})
	}
	p.Table("Origin does not match the canonical name", []string{"PATH", "ORIGIN", "ON PROVIDER"}, rows)
	fmt.Fprintln(p.Out)
}

func printBranches(p *render.Printer, report Report, showAll bool) {
	rows := make([][]render.Cell, 0, len(report.Repos))
	for _, res := range report.Repos {
		if showAll || res.Status != StatusSynced {
			rows = append(rows, branchRow(res))
		}
	}

	if len(rows) == 0 {
		if !showAll && len(report.Repos) > 0 {
			return
		}
		p.Line(render.Grey, "Default branches are in sync.")
		fmt.Fprintln(p.Out)
		return
	}
	p.Table("Default branch status", []string{"REPOSITORY", "BRANCH", "STATUS", "LAST FETCH", "PATH"}, rows)
	fmt.Fprintln(p.Out)
}

func branchRow(res Result) []render.Cell {
	age, ageColor := render.FetchAge(res.FetchedAt, res.FetchKnown)
	status := statusCell(res)
	if res.Detail != "" {
		status.Text = render.Ellipsis(status.Text+" ("+res.Detail+")", maxDetailWidth)
	}

	return []render.Cell{
		{Text: repoLabel(res), Color: render.Bold},
		render.Plain(res.Branch),
		status,
		{Text: age, Color: ageColor},
		{Text: render.Ellipsis(res.Path, maxPathWidth), Color: render.Grey},
	}
}

func repoLabel(res Result) string {
	if res.OriginName != "" {
		return res.FullName + " (origin: " + res.OriginName + ")"
	}

	return res.FullName
}

func printSummary(p *render.Printer, report Report, f filters) {
	counts := map[Status]int{}
	for _, res := range report.Repos {
		counts[res.Status]++
	}
	line := fmt.Sprintf("Summary: in sync — %d · %s · %s · %s · local only — %d · remote only — %d",
		counts[StatusSynced],
		p.Colored(render.Orange, fmt.Sprintf("need pull — %d", counts[StatusPull])),
		p.Colored(render.Yellow, fmt.Sprintf("need push — %d", counts[StatusPush])),
		p.Colored(render.Red, fmt.Sprintf("conflicts — %d", counts[StatusConflict])),
		len(report.LocalOnly),
		len(report.RemoteOnly),
	)
	if f.any() {
		line += " · filter: " + f.names()
	}
	p.Line("", "%s", line)
}
