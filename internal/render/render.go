package render

import (
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
	"unsafe"
)

const (
	Reset   = "\x1b[0m"
	Red     = "\x1b[31m"
	Orange  = "\x1b[38;5;208m"
	Yellow  = "\x1b[33m"
	Green   = "\x1b[32m"
	Blue    = "\x1b[34m"
	Magenta = "\x1b[35m"
	Grey    = "\x1b[90m"
	Bold    = "\x1b[1m"
)

const minColumnWidth = 12

type Cell struct {
	Text  string
	Color string
}

func Plain(text string) Cell { return Cell{Text: text} }

type Printer struct {
	Out   io.Writer
	Color bool
	Width int
}

func NewPrinter(out io.Writer, color bool) *Printer {
	return &Printer{Out: out, Color: color}
}

func ColorEnabled(force, disable bool, out *os.File) bool {
	if disable || os.Getenv("NO_COLOR") != "" {
		return false
	}
	if force {
		return true
	}
	info, err := out.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (p *Printer) paint(color, text string) string {
	if !p.Color || color == "" || text == "" {
		return text
	}
	return color + text + Reset
}

func (p *Printer) Line(color, format string, args ...any) {
	fmt.Fprintln(p.Out, p.paint(color, fmt.Sprintf(format, args...)))
}

func (p *Printer) Table(title string, headers []string, rows [][]Cell) {
	if len(rows) == 0 {
		return
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				continue
			}
			if w := utf8.RuneCountInString(cell.Text); w > widths[i] {
				widths[i] = w
			}
		}
	}
	fit(widths, p.Width)
	if title != "" {
		want := utf8.RuneCountInString(title) + 4
		if p.Width > 0 && want > p.Width-2 {
			want = p.Width - 2
		}
		grow(widths, want)
	}

	if title != "" {
		fmt.Fprintln(p.Out, p.titleLine(title, widths))
		fmt.Fprintln(p.Out, rule(widths, "├", "┬", "┤"))
	} else {
		fmt.Fprintln(p.Out, rule(widths, "┌", "┬", "┐"))
	}
	head := make([]Cell, len(headers))
	for i, h := range headers {
		head[i] = Cell{Text: h, Color: Bold}
	}
	fmt.Fprintln(p.Out, p.row(head, widths))
	fmt.Fprintln(p.Out, rule(widths, "├", "┼", "┤"))
	for _, row := range rows {
		fmt.Fprintln(p.Out, p.row(row, widths))
	}
	fmt.Fprintln(p.Out, rule(widths, "└", "┴", "┘"))
}

func (p *Printer) row(cells []Cell, widths []int) string {
	out := make([]string, 0, len(widths))
	for i, width := range widths {
		cell := Cell{}
		if i < len(cells) {
			cell = cells[i]
		}
		text := Ellipsis(cell.Text, width)
		out = append(out, " "+p.paint(cell.Color, text)+strings.Repeat(" ", gap(text, width))+" ")
	}
	return "│" + strings.Join(out, "│") + "│"
}

func (p *Printer) titleLine(title string, widths []int) string {
	rest := inner(widths) - utf8.RuneCountInString(title) - 3
	if rest < 0 {
		rest = 0
	}
	return "┌─ " + p.paint(Bold, title) + " " + strings.Repeat("─", rest) + "┐"
}

func rule(widths []int, left, mid, right string) string {
	parts := make([]string, 0, len(widths))
	for _, width := range widths {
		parts = append(parts, strings.Repeat("─", width+2))
	}
	return left + strings.Join(parts, mid) + right
}

func inner(widths []int) int {
	total := len(widths) - 1
	for _, width := range widths {
		total += width + 2
	}
	return total
}

func grow(widths []int, want int) {
	if len(widths) == 0 {
		return
	}
	if extra := want - inner(widths); extra > 0 {
		widths[len(widths)-1] += extra
	}
}

func fit(widths []int, limit int) {
	if limit <= 0 {
		return
	}
	for inner(widths)+2 > limit {
		i, max := -1, minColumnWidth
		for j, width := range widths {
			if width > max {
				i, max = j, width
			}
		}
		if i < 0 {
			return
		}
		widths[i]--
	}
}

func gap(text string, width int) int {
	if g := width - utf8.RuneCountInString(text); g > 0 {
		return g
	}
	return 0
}

func FetchAge(at time.Time, known bool) (string, string) {
	if !known {
		return "never", Red
	}
	d := time.Since(at)
	return humanAge(d), AgeColor(d)
}

func AgeColor(d time.Duration) string {
	switch {
	case d >= 30*24*time.Hour:
		return Red
	case d >= 10*24*time.Hour:
		return Orange
	case d >= 3*24*time.Hour:
		return Yellow
	default:
		return ""
	}
}

func humanAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (p *Printer) Colored(color, text string) string {
	return p.paint(color, text)
}

func Colorize(enabled bool, color, text string) string {
	if !enabled || color == "" || text == "" {
		return text
	}
	return color + text + Reset
}

func TerminalWidth(f *os.File) int {
	var size struct {
		rows, cols, xpixel, ypixel uint16
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&size)))
	if errno != 0 || size.cols == 0 {
		return 0
	}
	return int(size.cols)
}

func Ellipsis(s string, max int) string {
	if max <= 1 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return "…" + string(runes[len(runes)-(max-1):])
}
