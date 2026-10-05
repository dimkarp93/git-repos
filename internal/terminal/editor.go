package terminal

type Editor struct {
	text []rune
}

func NewEditor(text string) *Editor {
	return &Editor{text: []rune(text)}
}

func (e *Editor) String() string { return string(e.text) }

func (e *Editor) Handle(key Key) bool {
	switch key.Kind {
	case KeyRune:
		e.text = append(e.text, key.Rune)
		return true
	case KeyBackspace:
		if len(e.text) == 0 {
			return false
		}
		e.text = e.text[:len(e.text)-1]
		return true
	}
	return false
}

func (e *Editor) Line(prefix string) string {
	return prefix + string(e.text) + Reverse + " " + Reset
}
