package terminal

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func pickerAfter(keys ...Key) *Picker {
	p := testPicker()
	feed(p, keys...)
	return p
}

func TestPickerSelection(t *testing.T) {
	cases := map[string]struct {
		keys []Key
		want int
	}{
		"down":            {keys: []Key{keyOf(KeyDown)}, want: 1},
		"stays in range":  {keys: []Key{keyOf(KeyDown), keyOf(KeyDown), keyOf(KeyDown)}, want: 2},
		"up":              {keys: []Key{keyOf(KeyDown), keyOf(KeyDown), keyOf(KeyDown), keyOf(KeyUp)}, want: 1},
		"filter resets":   {keys: append([]Key{keyOf(KeyDown)}, typed("A")...), want: 0},
		"up at the start": {keys: []Key{keyOf(KeyUp)}, want: 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, pickerAfter(tc.keys...).index)
		})
	}
}

func TestPickerDownKeepsRunning(t *testing.T) {
	done, cancelled := feed(testPicker(), keyOf(KeyDown))

	assert.Equal(t, []bool{false, false}, []bool{done, cancelled})
}

func TestPickerFilter(t *testing.T) {
	cases := map[string]struct {
		filter string
		want   []string
	}{
		"case insensitive": {filter: "A", want: []string{"alpha", "beta", "gamma"}},
		"narrows":          {filter: "Am", want: []string{"gamma"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, pickerAfter(typed(tc.filter)...).Visible())
		})
	}
}

func TestPickerEnterOnEmptyListDoesNothing(t *testing.T) {
	p := pickerAfter(append(typed("zzz"), keyOf(KeyEnter))...)

	assert.False(t, p.choosing)
}

func TestPickerCancel(t *testing.T) {
	for _, key := range []KeyKind{KeyEsc, KeyCtrlC} {
		t.Run(fmt.Sprint(key), func(t *testing.T) {
			_, cancelled := feed(testPicker(), keyOf(key))
			assert.True(t, cancelled)
		})
	}
}

func choosingBeta() *Picker {
	return pickerAfter(keyOf(KeyDown), keyOf(KeyEnter))
}

func TestPickerNameStepStarts(t *testing.T) {
	p := choosingBeta()

	assert.Equal(t, []any{true, "beta", "beta"}, []any{p.choosing, p.chosen, p.input.String()})
}

func TestPickerNameStepRejectsDefault(t *testing.T) {
	p := choosingBeta()
	done, _ := feed(p, keyOf(KeyEnter))

	t.Run("not done", func(t *testing.T) {
		assert.False(t, done)
	})
	t.Run("problem shown", func(t *testing.T) {
		assert.NotEmpty(t, p.problem)
	})
	t.Run("problem is red", func(t *testing.T) {
		lines := p.Lines()
		assert.Contains(t, lines[len(lines)-1], Red)
	})
}

func TestPickerNameStepClearsProblem(t *testing.T) {
	p := choosingBeta()
	feed(p, keyOf(KeyEnter), keyOf(KeyUp))

	assert.Empty(t, p.problem)
}

func retype(text string) []Key {
	keys := slices.Repeat([]Key{keyOf(KeyBackspace)}, 10)
	return append(keys, typed(text)...)
}

func TestPickerNameStepRejectsTaken(t *testing.T) {
	p := choosingBeta()
	feed(p, append(retype("taken"), keyOf(KeyEnter))...)

	assert.NotEmpty(t, p.problem)
}

func TestPickerNameStepAcceptsFresh(t *testing.T) {
	p := choosingBeta()

	done, cancelled := feed(p, append(retype("fresh"), keyOf(KeyEnter))...)

	assert.Equal(t, []any{true, false, "fresh"}, []any{done, cancelled, p.input.String()})
}

func TestPickerEscReturnsToSelection(t *testing.T) {
	p := pickerAfter(append(typed("a"), keyOf(KeyDown), keyOf(KeyEnter))...)
	done, cancelled := feed(p, keyOf(KeyEsc))

	t.Run("back to selection", func(t *testing.T) {
		assert.Equal(t, []bool{false, false, false}, []bool{done, cancelled, p.choosing})
	})
	t.Run("filter kept", func(t *testing.T) {
		assert.Equal(t, "a", p.filter.String())
	})
}

type scriptedKeys struct {
	batches [][]Key
	next    int
}

func (s *scriptedKeys) read() ([]Key, error) {
	batch := s.batches[s.next]
	s.next++
	return batch, nil
}

func TestPickerRun(t *testing.T) {
	keys := &scriptedKeys{batches: [][]Key{{keyOf(KeyDown)}, {keyOf(KeyEnter)}, typed("2"), {keyOf(KeyEnter)}}}
	var out bytes.Buffer

	oldName, newName, cancelled, err := testPicker().Run(keys.read, NewScreen(&out))

	t.Run("result", func(t *testing.T) {
		require.NoError(t, err)
		assert.Equal(t, []any{"beta", "beta2", false}, []any{oldName, newName, cancelled})
	})
	t.Run("draws the table", func(t *testing.T) {
		assert.Contains(t, out.String(), "┌")
	})
}

func TestPickerRunCancelled(t *testing.T) {
	keys := &scriptedKeys{batches: [][]Key{{keyOf(KeyEsc)}}}

	_, _, cancelled, err := testPicker().Run(keys.read, NewScreen(&bytes.Buffer{}))

	require.NoError(t, err)
	assert.True(t, cancelled)
}

func TestPickerRunReadError(t *testing.T) {
	failing := func() ([]Key, error) { return nil, errors.New("read failed") }

	_, _, _, err := testPicker().Run(failing, NewScreen(&bytes.Buffer{}))

	assert.Error(t, err)
}
