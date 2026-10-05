package terminal

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"
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
		got := kinds(ParseKeys([]byte(tc.in)))
		if len(got) != len(tc.want) {
			t.Errorf("ParseKeys(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ParseKeys(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
	if keys := ParseKeys([]byte("я")); keys[0].Rune != 'я' {
		t.Errorf("rune = %q", keys[0].Rune)
	}
}

func TestEditor(t *testing.T) {
	e := NewEditor("ab")
	if !e.Handle(Key{Kind: KeyRune, Rune: 'я'}) || e.String() != "abя" {
		t.Fatalf("text = %q", e.String())
	}
	if !e.Handle(Key{Kind: KeyBackspace}) || e.String() != "ab" {
		t.Fatalf("text = %q", e.String())
	}
	if e.Handle(Key{Kind: KeyUp}) {
		t.Fatal("arrow must not change the text")
	}
	empty := NewEditor("")
	if empty.Handle(Key{Kind: KeyBackspace}) {
		t.Fatal("backspace on empty text must not change it")
	}
}

func plain(s string) string {
	return strings.NewReplacer(Reverse, "", Reset, "").Replace(s)
}

func TestGridLayout(t *testing.T) {
	items := make([]string, 0, 20)
	for i := 0; i < 20; i++ {
		items = append(items, "r"+string(rune('a'+i)))
	}
	lines := Grid(items, 0, 2, 30)
	if len(lines) != 12 {
		t.Fatalf("lines = %d, want 12", len(lines))
	}
	width := utf8.RuneCountInString(plain(lines[0]))
	if width != 1+32+1+32+1 {
		t.Fatalf("width = %d", width)
	}
	for _, line := range lines {
		if got := utf8.RuneCountInString(plain(line)); got != width {
			t.Fatalf("line %q width = %d, want %d", plain(line), got, width)
		}
	}
	if !strings.HasPrefix(plain(lines[1]), "│ ra ") || !strings.Contains(plain(lines[1]), "│ rk ") {
		t.Fatalf("first row = %q", plain(lines[1]))
	}
	if !strings.Contains(lines[1], Reverse) || strings.Contains(lines[2], Reverse) {
		t.Fatal("only the selected cell must be highlighted")
	}
}

func TestGridTruncatesAndHandlesEmpty(t *testing.T) {
	long := strings.Repeat("x", 40)
	lines := Grid([]string{long}, 0, 2, 30)
	if !strings.Contains(plain(lines[1]), strings.Repeat("x", 29)+"…") {
		t.Fatalf("row = %q", plain(lines[1]))
	}
	empty := Grid(nil, 0, 2, 30)
	if len(empty) != 3 || strings.Contains(empty[1], Reverse) {
		t.Fatalf("empty grid = %q", empty)
	}
}

func TestScreenRedraw(t *testing.T) {
	var out bytes.Buffer
	s := NewScreen(&out)
	s.Draw([]string{"a", "b", "c"})
	out.Reset()
	s.Draw([]string{"x"})
	if got := out.String(); got != "\x1b[2A\r\x1b[Jx" {
		t.Fatalf("redraw = %q", got)
	}
	out.Reset()
	s.Clear()
	if got := out.String(); got != "\r\x1b[J" {
		t.Fatalf("clear = %q", got)
	}
	out.Reset()
	s.Clear()
	if out.Len() != 0 {
		t.Fatalf("second clear wrote %q", out.String())
	}
}
