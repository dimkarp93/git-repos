package app

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dimkarp93/git-repos/internal/render"
)

type statusGroup struct {
	name   string
	header string
	key    func(statusResult) (id, label string)
}

func statusGroups() []statusGroup {
	return []statusGroup{
		{"repo", "REPOSITORY", func(r statusResult) (string, string) { return r.repo, r.name }},
		{"branch", "BRANCH", func(r statusResult) (string, string) { return r.branch, r.branch }},
		{"worktree", "WORKSPACE", func(r statusResult) (string, string) { return r.workspace, r.workspace }},
	}
}

func statusGroupNames() []string {
	groups := statusGroups()
	names := make([]string, len(groups))
	for i, g := range groups {
		names[i] = g.name
	}
	return names
}

func lookupStatusGroup(name string) (statusGroup, error) {
	for _, g := range statusGroups() {
		if g.name == name {
			return g, nil
		}
	}
	return statusGroup{}, fmt.Errorf("--group accepts %s", strings.Join(statusGroupNames(), ", "))
}

type groupCount struct {
	id    string
	label string
	count int
}

func countGroups(results []statusResult, f filters, g statusGroup) []groupCount {
	index := map[string]int{}
	var out []groupCount
	for _, res := range results {
		if !f.matchStatus(res) {
			continue
		}
		id, label := g.key(res)
		if i, ok := index[id]; ok {
			out[i].count++
			continue
		}
		index[id] = len(out)
		out = append(out, groupCount{id: id, label: label, count: 1})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].label != out[j].label {
			return out[i].label < out[j].label
		}
		return out[i].id < out[j].id
	})
	return out
}

func groupRows(counts []groupCount) [][]render.Cell {
	rows := make([][]render.Cell, 0, len(counts))
	for _, c := range counts {
		label := c.label
		if label == "" {
			label = "-"
		}
		rows = append(rows, []render.Cell{
			{Text: label, Color: render.Bold},
			render.Plain(strconv.Itoa(c.count)),
		})
	}
	return rows
}
