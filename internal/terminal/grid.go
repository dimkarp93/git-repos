package terminal

import (
	"strings"
	"unicode/utf8"
)

func Grid(items []string, selected, columns, cellWidth int) []string {
	rows := (len(items) + columns - 1) / columns
	if rows < 1 {
		rows = 1
	}
	segment := strings.Repeat("─", cellWidth+2)
	rule := func(left, mid, right string) string {
		parts := make([]string, columns)
		for i := range parts {
			parts[i] = segment
		}
		return left + strings.Join(parts, mid) + right
	}
	lines := []string{rule("┌", "┬", "┐")}
	for row := 0; row < rows; row++ {
		var b strings.Builder
		b.WriteString("│")
		for col := 0; col < columns; col++ {
			index := col*rows + row
			text := ""
			if index < len(items) {
				text = items[index]
			}
			cell := " " + pad(text, cellWidth) + " "
			if index == selected && index < len(items) {
				cell = Reverse + cell + Reset
			}
			b.WriteString(cell)
			b.WriteString("│")
		}
		lines = append(lines, b.String())
	}
	return append(lines, rule("└", "┴", "┘"))
}

func pad(text string, width int) string {
	n := utf8.RuneCountInString(text)
	if n > width {
		runes := []rune(text)
		return string(runes[:width-1]) + "…"
	}
	return text + strings.Repeat(" ", width-n)
}
