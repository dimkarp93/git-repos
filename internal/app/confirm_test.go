package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestConfirmerAnswers(t *testing.T) {
	var out bytes.Buffer
	c := newScriptedConfirmer(strings.NewReader("yes\nskip\nskip-to-all\n"), &out, false)
	for _, want := range []answer{answerYes, answerSkip, answerQuit} {
		got, err := c.ask("?")
		if err != nil || got != want {
			t.Fatalf("ask = %v, %v; want %v", got, err, want)
		}
	}
}

func TestConfirmerYesToAllSticks(t *testing.T) {
	var out bytes.Buffer
	c := newScriptedConfirmer(strings.NewReader("yes-to-all\n"), &out, false)
	for i := range 3 {
		got, err := c.ask("?")
		if err != nil || got != answerYes {
			t.Fatalf("ask #%d = %v, %v", i, got, err)
		}
	}
	if strings.Count(out.String(), "[yes | yes-to-all | skip | skip-to-all]") != 1 {
		t.Fatalf("prompt repeated: %q", out.String())
	}
}

func TestConfirmerRequiresExplicitAnswer(t *testing.T) {
	var out bytes.Buffer
	c := newScriptedConfirmer(strings.NewReader("\n  \nmaybe\nyes\n"), &out, false)
	got, err := c.ask("?")
	if err != nil || got != answerYes {
		t.Fatalf("ask = %v, %v", got, err)
	}
	if !strings.Contains(out.String(), "pick an option explicitly") {
		t.Errorf("no hint for empty answer: %q", out.String())
	}
	if !strings.Contains(out.String(), "unknown answer") {
		t.Errorf("no hint for unknown answer: %q", out.String())
	}
	if strings.Count(out.String(), "[yes | yes-to-all | skip | skip-to-all]") != 4 {
		t.Errorf("prompt count = %q", out.String())
	}
}

func TestConfirmerAssumeYesSkipsPrompt(t *testing.T) {
	var out bytes.Buffer
	c := newScriptedConfirmer(strings.NewReader(""), &out, true)
	got, err := c.ask("?")
	if err != nil || got != answerYes {
		t.Fatalf("ask = %v, %v", got, err)
	}
	if out.Len() != 0 {
		t.Fatalf("prompted with --yes: %q", out.String())
	}
}

func TestConfirmerEOFQuits(t *testing.T) {
	var out bytes.Buffer
	c := newScriptedConfirmer(strings.NewReader(""), &out, false)
	got, err := c.ask("?")
	if err != nil || got != answerQuit {
		t.Fatalf("ask = %v, %v", got, err)
	}
}

func TestConfirmerWithoutTTYFails(t *testing.T) {
	c := &confirmer{out: &bytes.Buffer{}}
	if _, err := c.ask("?"); err != errNotInteractive {
		t.Fatalf("err = %v", err)
	}
}

func TestConfirmerChoose(t *testing.T) {
	var out bytes.Buffer
	c := newScriptedConfirmer(strings.NewReader("\n0\n4\n2x\n2\nskip\nq\n"), &out, false)
	idx, ans, err := c.choose("?", 3)
	if err != nil || ans != answerYes || idx != 1 {
		t.Fatalf("choose = %d, %v, %v", idx, ans, err)
	}
	if _, ans, _ := c.choose("?", 3); ans != answerSkip {
		t.Fatalf("second = %v", ans)
	}
	if _, ans, _ := c.choose("?", 3); ans != answerQuit {
		t.Fatalf("third = %v", ans)
	}
	if !strings.Contains(out.String(), "[1..3 | skip | skip-to-all]") {
		t.Fatalf("prompt = %q", out.String())
	}
}

func TestConfirmerAskTextDefaultAndValidation(t *testing.T) {
	var out bytes.Buffer
	taken := func(name string) error {
		if name == "origin" {
			return errors.New("remote origin already exists")
		}
		return nil
	}
	c := newScriptedConfirmer(strings.NewReader("origin\n\n"), &out, false)
	got, ans, err := c.askText("Remote name", "github", taken)
	if err != nil || ans != answerYes || got != "github" {
		t.Fatalf("askText = %q, %v, %v", got, ans, err)
	}
	if !strings.Contains(out.String(), "remote origin already exists") {
		t.Fatalf("out = %q", out.String())
	}
}

func TestConfirmerAskTextYesToAll(t *testing.T) {
	c := newScriptedConfirmer(strings.NewReader(""), &bytes.Buffer{}, true)
	if got, ans, err := c.askText("?", "github", func(string) error { return nil }); got != "github" || ans != answerYes || err != nil {
		t.Fatalf("askText = %q, %v, %v", got, ans, err)
	}
	if _, ans, err := c.askText("?", "github", func(string) error { return errors.New("taken") }); ans != answerSkip || err == nil {
		t.Fatalf("askText = %v, %v", ans, err)
	}
}

func TestConfirmerOption(t *testing.T) {
	var out bytes.Buffer
	c := newScriptedConfirmer(strings.NewReader("\nx\nO\nrename\n"), &out, false)
	if got, ok, err := c.option("?", "overwrite", "rename"); got != "overwrite" || !ok || err != nil {
		t.Fatalf("option = %q, %v, %v", got, ok, err)
	}
	if got, _, _ := c.option("?", "overwrite", "rename"); got != "rename" {
		t.Fatalf("second = %q", got)
	}
	if _, ok, _ := c.option("?", "overwrite", "rename"); ok {
		t.Fatal("EOF accepted")
	}
}
