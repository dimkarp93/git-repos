package app

import (
	"bytes"
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
