package terminal

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func keyOf(kind KeyKind) Key { return Key{Kind: kind} }

func typed(text string) []Key {
	var keys []Key
	for _, r := range text {
		keys = append(keys, Key{Kind: KeyRune, Rune: r})
	}
	return keys
}

func feed(p *Picker, keys ...Key) (done, cancelled bool) {
	for _, key := range keys {
		if done, cancelled = p.Handle(key); done || cancelled {
			return
		}
	}
	return
}

func testPicker() *Picker {
	return NewPicker([]string{"alpha", "beta", "gamma"}, func(oldName, newName string) error {
		if newName == "taken" || newName == oldName {
			return errors.New("name taken")
		}
		return nil
	})
}

func TestPickerFilterAndSelection(t *testing.T) {
	p := testPicker()
	if done, cancelled := feed(p, keyOf(KeyDown)); done || cancelled || p.index != 1 {
		t.Fatalf("index = %d", p.index)
	}
	feed(p, keyOf(KeyDown), keyOf(KeyDown))
	if p.index != 2 {
		t.Fatalf("index must stay in range, got %d", p.index)
	}
	feed(p, keyOf(KeyUp))
	if p.index != 1 {
		t.Fatalf("index = %d", p.index)
	}
	feed(p, typed("A")...)
	if got := p.Visible(); len(got) != 3 || p.index != 0 {
		t.Fatalf("visible = %v, index = %d", got, p.index)
	}
	feed(p, typed("m")...)
	if got := p.Visible(); len(got) != 1 || got[0] != "gamma" {
		t.Fatalf("visible = %v", got)
	}
}

func TestPickerEnterOnEmptyListDoesNothing(t *testing.T) {
	p := testPicker()
	feed(p, typed("zzz")...)
	feed(p, keyOf(KeyEnter))
	if p.choosing {
		t.Fatal("must stay on the selection step")
	}
}

func TestPickerCancel(t *testing.T) {
	for _, key := range []KeyKind{KeyEsc, KeyCtrlC} {
		if _, cancelled := feed(testPicker(), keyOf(key)); !cancelled {
			t.Errorf("key %v did not cancel", key)
		}
	}
}

func TestPickerNameStep(t *testing.T) {
	p := testPicker()
	feed(p, keyOf(KeyDown), keyOf(KeyEnter))
	if !p.choosing || p.chosen != "beta" || p.input.String() != "beta" {
		t.Fatalf("choosing = %v, chosen = %q, input = %q", p.choosing, p.chosen, p.input.String())
	}
	if done, _ := feed(p, keyOf(KeyEnter)); done || p.problem == "" {
		t.Fatalf("default name must fail validation, problem = %q", p.problem)
	}
	if lines := p.Lines(); !strings.Contains(lines[len(lines)-1], Red) {
		t.Fatalf("problem must be red: %q", lines)
	}
	feed(p, keyOf(KeyUp))
	if p.problem != "" {
		t.Fatal("any key must clear the problem")
	}
	feed(p, keyOf(KeyBackspace), keyOf(KeyBackspace), keyOf(KeyBackspace), keyOf(KeyBackspace))
	feed(p, typed("taken")...)
	feed(p, keyOf(KeyEnter))
	if p.problem == "" {
		t.Fatal("taken name must fail validation")
	}
	feed(p, keyOf(KeyBackspace))
	feed(p, typed("n")...)
	feed(p, keyOf(KeyBackspace), keyOf(KeyBackspace), keyOf(KeyBackspace), keyOf(KeyBackspace), keyOf(KeyBackspace))
	feed(p, typed("fresh")...)
	if done, cancelled := feed(p, keyOf(KeyEnter)); !done || cancelled || p.input.String() != "fresh" {
		t.Fatalf("done = %v, cancelled = %v, input = %q", done, cancelled, p.input.String())
	}
}

func TestPickerEscReturnsToSelection(t *testing.T) {
	p := testPicker()
	feed(p, typed("a")...)
	feed(p, keyOf(KeyDown), keyOf(KeyEnter))
	if done, cancelled := feed(p, keyOf(KeyEsc)); done || cancelled || p.choosing {
		t.Fatal("Esc must return to the selection step")
	}
	if p.filter.String() != "a" {
		t.Fatalf("filter = %q, must be kept", p.filter.String())
	}
}

func TestPickerRun(t *testing.T) {
	p := testPicker()
	script := [][]Key{
		{keyOf(KeyDown)},
		{keyOf(KeyEnter)},
		typed("2"),
		{keyOf(KeyEnter)},
	}
	var out bytes.Buffer
	next := 0
	keys := func() ([]Key, error) {
		batch := script[next]
		next++
		return batch, nil
	}
	oldName, newName, cancelled, err := p.Run(keys, NewScreen(&out))
	if err != nil || cancelled || oldName != "beta" || newName != "beta2" {
		t.Fatalf("got %q → %q, cancelled = %v, err = %v", oldName, newName, cancelled, err)
	}
	if !strings.Contains(out.String(), "┌") {
		t.Fatalf("the table was not drawn: %q", out.String())
	}
}

func TestPickerRunCancelled(t *testing.T) {
	p := testPicker()
	keys := func() ([]Key, error) { return []Key{keyOf(KeyEsc)}, nil }
	_, _, cancelled, err := p.Run(keys, NewScreen(&bytes.Buffer{}))
	if err != nil || !cancelled {
		t.Fatalf("cancelled = %v, err = %v", cancelled, err)
	}
}
