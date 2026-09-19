package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

type answer int

const (
	answerYes answer = iota
	answerSkip
	answerQuit
)

var errNotInteractive = errors.New("no terminal to confirm on, use --yes")

type confirmer struct {
	in       *bufio.Reader
	out      io.Writer
	yesToAll bool
	tty      bool
}

func newConfirmer(assumeYes bool) *confirmer {
	info, err := os.Stdin.Stat()
	tty := err == nil && info.Mode()&os.ModeCharDevice != 0
	return &confirmer{in: bufio.NewReader(os.Stdin), out: os.Stdout, yesToAll: assumeYes, tty: tty}
}

func newScriptedConfirmer(in io.Reader, out io.Writer, assumeYes bool) *confirmer {
	return &confirmer{in: bufio.NewReader(in), out: out, yesToAll: assumeYes, tty: true}
}

func (c *confirmer) ask(prompt string) (answer, error) {
	if c.yesToAll {
		return answerYes, nil
	}
	if !c.tty {
		return answerQuit, errNotInteractive
	}
	for {
		fmt.Fprintf(c.out, "%s\n  [yes | yes-to-all | skip | skip-to-all]: ", prompt)
		line, err := c.in.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(c.out)
				return answerQuit, nil
			}
			return answerQuit, err
		}
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "yes", "y":
			return answerYes, nil
		case "yes-to-all", "a":
			c.yesToAll = true
			return answerYes, nil
		case "skip", "s":
			return answerSkip, nil
		case "skip-to-all", "q":
			return answerQuit, nil
		case "":
			fmt.Fprintln(c.out, "  pick an option explicitly")
		default:
			fmt.Fprintln(c.out, "  unknown answer, pick one of the options")
		}
	}
}
