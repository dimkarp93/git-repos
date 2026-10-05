package terminal

import (
	"fmt"
	"io"
	"strings"
)

const (
	Reverse = "\x1b[7m"
	Reset   = "\x1b[0m"
)

type Screen struct {
	out    io.Writer
	height int
}

func NewScreen(out io.Writer) *Screen {
	return &Screen{out: out}
}

func (s *Screen) Hide() { fmt.Fprint(s.out, "\x1b[?25l") }

func (s *Screen) Show() { fmt.Fprint(s.out, "\x1b[?25h") }

func (s *Screen) Draw(lines []string) {
	s.Clear()
	fmt.Fprint(s.out, strings.Join(lines, "\r\n"))
	s.height = len(lines)
}

func (s *Screen) Clear() {
	if s.height == 0 {
		return
	}
	if s.height > 1 {
		fmt.Fprintf(s.out, "\x1b[%dA", s.height-1)
	}
	fmt.Fprint(s.out, "\r\x1b[J")
	s.height = 0
}
