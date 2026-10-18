package terminal

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kinds(keys []Key) []KeyKind {
	out := make([]KeyKind, len(keys))
	for i, k := range keys {
		out[i] = k.Kind
	}
	return out
}

func TestParseKeys(t *testing.T) {
	cases := []struct {
		in   string
		want []KeyKind
	}{
		{"\x1b", []KeyKind{KeyEsc}},
		{"\x1b[A", []KeyKind{KeyUp}},
		{"\x1b[B", []KeyKind{KeyDown}},
		{"\x1bOB", []KeyKind{KeyDown}},
		{"\x1b[C\x1b[D", []KeyKind{KeyRight, KeyLeft}},
		{"\x1b[3~", []KeyKind{KeyOther}},
		{"\r", []KeyKind{KeyEnter}},
		{"\n", []KeyKind{KeyEnter}},
		{"\x7f", []KeyKind{KeyBackspace}},
		{"\x03", []KeyKind{KeyCtrlC}},
		{"\t", []KeyKind{KeyTab}},
		{"ab", []KeyKind{KeyRune, KeyRune}},
		{"я", []KeyKind{KeyRune}},
		{"a\x1b", []KeyKind{KeyRune, KeyEsc}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.in), func(t *testing.T) {
			assert.Equal(t, tc.want, kinds(ParseKeys([]byte(tc.in))))
		})
	}
}

func TestParseKeysDecodesRune(t *testing.T) {
	assert.Equal(t, []Key{{Kind: KeyRune, Rune: 'я'}}, ParseKeys([]byte("я")))
}

func TestEditor(t *testing.T) {
	cases := map[string]struct {
		start   string
		key     Key
		changed bool
		want    string
	}{
		"insert rune":     {start: "ab", key: Key{Kind: KeyRune, Rune: 'я'}, changed: true, want: "abя"},
		"backspace":       {start: "abя", key: Key{Kind: KeyBackspace}, changed: true, want: "ab"},
		"arrow":           {start: "ab", key: Key{Kind: KeyUp}, want: "ab"},
		"backspace empty": {start: "", key: Key{Kind: KeyBackspace}, want: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			e := NewEditor(tc.start)
			changed := e.Handle(tc.key)
			assert.Equal(t, []any{tc.changed, tc.want}, []any{changed, e.String()})
		})
	}
}

func plain(s string) string {
	return strings.NewReplacer(Reverse, "", Reset, "").Replace(s)
}

func gridItems(n int) []string {
	items := make([]string, 0, n)
	for i := range n {
		items = append(items, "r"+string(rune('a'+i)))
	}
	return items
}

func gridLines(t *testing.T) []string {
	t.Helper()

	lines := Grid(gridItems(20), 0, 2, 30)
	require.Len(t, lines, 12)

	return lines
}

func TestGridLayout(t *testing.T) {
	lines := gridLines(t)

	t.Run("equal widths", func(t *testing.T) {
		for _, line := range lines {
			assert.Equal(t, 1+32+1+32+1, utf8.RuneCountInString(plain(line)))
		}
	})
	t.Run("column order", func(t *testing.T) {
		assert.Regexp(t, `^│ ra .*│ rk `, plain(lines[1]))
	})
	t.Run("selected highlighted", func(t *testing.T) {
		assert.Contains(t, lines[1], Reverse)
	})
	t.Run("others plain", func(t *testing.T) {
		assert.NotContains(t, lines[2], Reverse)
	})
}

func TestGridTruncates(t *testing.T) {
	lines := Grid([]string{strings.Repeat("x", 40)}, 0, 2, 30)

	assert.Contains(t, plain(lines[1]), strings.Repeat("x", 29)+"…")
}

func TestGridEmpty(t *testing.T) {
	empty := Grid(nil, 0, 2, 30)

	require.Len(t, empty, 3)
	assert.NotContains(t, empty[1], Reverse)
}

func screenOutputs(steps ...func(*Screen)) []string {
	var out bytes.Buffer
	s := NewScreen(&out)
	got := make([]string, 0, len(steps))
	for _, step := range steps {
		out.Reset()
		step(s)
		got = append(got, out.String())
	}
	return got
}

func drawLines(lines ...string) func(*Screen) {
	return func(s *Screen) { s.Draw(lines) }
}

func clearScreen(s *Screen) { s.Clear() }

func TestScreenRedraw(t *testing.T) {
	got := screenOutputs(drawLines("a", "b", "c"), drawLines("x"), clearScreen, clearScreen)

	assert.Equal(t, []string{"a\r\nb\r\nc", "\x1b[2A\r\x1b[Jx", "\r\x1b[J", ""}, got)
}
