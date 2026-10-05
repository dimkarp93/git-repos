package terminal

import "strings"

const Red = "\x1b[31m"

type Picker struct {
	Items        []string
	Columns      int
	CellWidth    int
	SelectPrompt string
	InputPrompt  string
	TitlePrefix  string
	Validate     func(chosen, value string) error

	filter   *Editor
	index    int
	choosing bool
	chosen   string
	input    *Editor
	problem  string
}

func NewPicker(items []string, validate func(chosen, value string) error) *Picker {
	return &Picker{
		Items:        items,
		Columns:      2,
		CellWidth:    30,
		SelectPrompt: "Select: ",
		InputPrompt:  "Value: ",
		TitlePrefix:  "Selected ",
		Validate:     validate,
		filter:       NewEditor(""),
	}
}

func (p *Picker) Visible() []string {
	needle := strings.ToLower(p.filter.String())
	out := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		if strings.Contains(strings.ToLower(item), needle) {
			out = append(out, item)
		}
	}
	return out
}

func (p *Picker) Handle(key Key) (done, cancelled bool) {
	if key.Kind == KeyCtrlC {
		return false, true
	}
	if p.choosing {
		return p.handleInput(key)
	}
	return p.handleSelect(key)
}

func (p *Picker) handleSelect(key Key) (done, cancelled bool) {
	visible := p.Visible()
	switch key.Kind {
	case KeyEsc:
		return false, true
	case KeyDown, KeyTab:
		if p.index < len(visible)-1 {
			p.index++
		}
	case KeyUp:
		if p.index > 0 {
			p.index--
		}
	case KeyEnter:
		if len(visible) > 0 {
			p.chosen = visible[p.index]
			p.input = NewEditor(p.chosen)
			p.choosing = true
			p.problem = ""
		}
	default:
		if p.filter.Handle(key) {
			p.index = 0
		}
	}
	return false, false
}

func (p *Picker) handleInput(key Key) (done, cancelled bool) {
	p.problem = ""
	switch key.Kind {
	case KeyEsc:
		p.choosing = false
		p.index = max(0, min(p.index, len(p.Visible())-1))
	case KeyEnter:
		if err := p.Validate(p.chosen, p.input.String()); err != nil {
			p.problem = err.Error()
			return false, false
		}
		return true, false
	default:
		p.input.Handle(key)
	}
	return false, false
}

func (p *Picker) Lines() []string {
	if p.choosing {
		lines := []string{
			p.TitlePrefix + p.chosen,
			p.input.Line(p.InputPrompt),
		}
		if p.problem != "" {
			lines = append(lines, Red+p.problem+Reset)
		}
		return lines
	}
	lines := []string{p.filter.Line(p.SelectPrompt)}
	return append(lines, Grid(p.Visible(), p.index, p.Columns, p.CellWidth)...)
}

func (p *Picker) Run(keys KeySource, screen *Screen) (chosen, value string, cancelled bool, err error) {
	screen.Hide()
	defer screen.Show()
	screen.Draw(p.Lines())
	for {
		batch, err := keys()
		if err != nil {
			screen.Clear()
			return "", "", false, err
		}
		for _, key := range batch {
			done, cancel := p.Handle(key)
			if cancel {
				screen.Clear()
				return "", "", true, nil
			}
			if done {
				screen.Clear()
				return p.chosen, p.input.String(), false, nil
			}
		}
		screen.Draw(p.Lines())
	}
}
