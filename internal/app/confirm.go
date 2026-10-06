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

func (c *confirmer) readLine() (string, bool, error) {
	line, err := c.in.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(c.out)
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(line), true, nil
}

func (c *confirmer) choose(prompt string, n int) (int, answer, error) {
	if !c.tty {
		return 0, answerQuit, errNotInteractive
	}
	options := "1"
	if n > 1 {
		options = fmt.Sprintf("1..%d", n)
	}
	for {
		fmt.Fprintf(c.out, "%s\n  [%s | skip | skip-to-all]: ", prompt, options)
		line, ok, err := c.readLine()
		if err != nil || !ok {
			return 0, answerQuit, err
		}
		switch strings.ToLower(line) {
		case "skip", "s":
			return 0, answerSkip, nil
		case "skip-to-all", "q":
			return 0, answerQuit, nil
		case "":
			fmt.Fprintln(c.out, "  pick an option explicitly")
			continue
		}
		var pick int
		if _, err := fmt.Sscanf(line, "%d", &pick); err == nil && fmt.Sprint(pick) == line && pick >= 1 && pick <= n {
			return pick - 1, answerYes, nil
		}
		fmt.Fprintln(c.out, "  unknown answer, pick one of the options")
	}
}

func (c *confirmer) askText(prompt, def string, valid func(string) error) (string, answer, error) {
	if c.yesToAll {
		if err := valid(def); err != nil {
			return "", answerSkip, err
		}
		return def, answerYes, nil
	}
	if !c.tty {
		return "", answerQuit, errNotInteractive
	}
	for {
		fmt.Fprintf(c.out, "%s [%s]: ", prompt, def)
		line, ok, err := c.readLine()
		if err != nil || !ok {
			return "", answerQuit, err
		}
		if line == "" {
			line = def
		}
		if err := valid(line); err != nil {
			fmt.Fprintf(c.out, "  %v\n", err)
			continue
		}
		return line, answerYes, nil
	}
}

func (c *confirmer) option(prompt string, choices ...string) (string, bool, error) {
	if !c.tty {
		return "", false, errNotInteractive
	}
	for {
		fmt.Fprintf(c.out, "%s\n  [%s]: ", prompt, strings.Join(choices, " | "))
		line, ok, err := c.readLine()
		if err != nil || !ok {
			return "", false, err
		}
		line = strings.ToLower(line)
		if line == "" {
			fmt.Fprintln(c.out, "  pick an option explicitly")
			continue
		}
		var matched []string
		for _, choice := range choices {
			if choice == line {
				return choice, true, nil
			}
			if strings.HasPrefix(choice, line) {
				matched = append(matched, choice)
			}
		}
		if len(matched) == 1 {
			return matched[0], true, nil
		}
		fmt.Fprintln(c.out, "  unknown answer, pick one of the options")
	}
}
