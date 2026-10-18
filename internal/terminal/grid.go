package terminal

import (
	"slices"
	"strings"
	"unicode/utf8"
)

func Grid(items []string, selected, columns, cellWidth int) []string {
	rows := max((len(items)+columns-1)/columns, 1)
	rule := gridRule(columns, cellWidth)

	lines := []string{rule("┌", "┬", "┐")}
	for row := 0; row < rows; row++ {
		lines = append(lines, gridRow(items, row, rows, selected, columns, cellWidth))
	}
	return append(lines, rule("└", "┴", "┘"))
}

func gridRule(columns, cellWidth int) func(left, mid, right string) string {
	segments := slices.Repeat([]string{strings.Repeat("─", cellWidth+2)}, columns)
	return func(left, mid, right string) string { return left + strings.Join(segments, mid) + right }
}

func gridRow(items []string, row, rows, selected, columns, cellWidth int) string {
	var b strings.Builder
	b.WriteString("│")
	for col := 0; col < columns; col++ {
		index := col*rows + row
		b.WriteString(gridCell(items, index, selected, cellWidth))
		b.WriteString("│")
	}
	return b.String()
}

func gridCell(items []string, index, selected, cellWidth int) string {
	if index >= len(items) {
		return " " + pad("", cellWidth) + " "
	}

	cell := " " + pad(items[index], cellWidth) + " "
	if index == selected {
		return Reverse + cell + Reset
	}
	return cell
}

func pad(text string, width int) string {
	n := utf8.RuneCountInString(text)
	if n > width {
		runes := []rune(text)
		return string(runes[:width-1]) + "…"
	}
	return text + strings.Repeat(" ", width-n)
}
