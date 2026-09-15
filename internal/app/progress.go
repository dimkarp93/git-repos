package app

import (
	"fmt"
	"strings"
	"sync"

	"github.com/dimkarp93/git-repos/internal/render"
)

const maxProgressItemWidth = 44

type progress struct {
	mu     sync.Mutex
	color  bool
	plan   int
	index  int
	phase  string
	unit   string
	item   string
	found  int
	done   int
	total  int
	active int
}

func (p *progress) setPlan(phases int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.plan = phases
	p.index = 0
}

func (p *progress) setPhase(phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.index++
	p.phase = phase
	p.unit = ""
	p.item = ""
	p.found = 0
	p.done = 0
	p.total = 0
	p.active = 0
}

func (p *progress) setUnit(unit string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unit = unit
}

func (p *progress) setTotal(total int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total = total
}

func (p *progress) note(item string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.item = item
}

func (p *progress) add(item string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.item = item
	p.found++
}

func (p *progress) step(item string, done int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.item = item
	if done > p.done {
		p.done = done
	}
}

func (p *progress) complete() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.item = ""
	p.done = p.total
}

func (p *progress) begin(item string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.item = item
	p.active++
}

func (p *progress) end() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active--
	p.done++
}

func (p *progress) Note(item string) { p.note(item) }

func (p *progress) Total(total int) { p.setTotal(total) }

func (p *progress) Done(done int) { p.step("", done) }

func (p *progress) paint(color, text string) string {
	return render.Colorize(p.color, color, text)
}

func (p *progress) label() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	parts := make([]string, 0, 5)
	head := ""
	if p.plan > 0 && p.index > 0 {
		head = fmt.Sprintf("фаза %d/%d · ", p.index, p.plan)
	}
	if p.phase != "" {
		head += p.phase
	}
	if head != "" {
		parts = append(parts, p.paint(render.Bold, strings.TrimSuffix(head, " · ")))
	}
	if p.item != "" {
		parts = append(parts, p.paint(render.Grey, render.Ellipsis(p.item, maxProgressItemWidth)))
	}
	switch {
	case p.total > 0:
		done := min(p.done, p.total)
		counts := fmt.Sprintf("%d из %d", done, p.total)
		if p.unit != "" {
			counts += " " + p.unit
		}
		parts = append(parts, p.paint(render.Green, fmt.Sprintf("%d%% (%s)", done*100/p.total, counts)))
	case p.found > 0:
		parts = append(parts, p.paint(render.Green, fmt.Sprintf("найдено — %d", p.found)))
	}
	if p.active > 1 {
		parts = append(parts, p.paint(render.Grey, fmt.Sprintf("в работе — %d", p.active)))
	}
	return strings.Join(parts, " · ")
}
