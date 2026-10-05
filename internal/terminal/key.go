package terminal

import (
	"os"
	"unicode/utf8"
)

type KeyKind int

const (
	KeyRune KeyKind = iota
	KeyEnter
	KeyEsc
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyBackspace
	KeyTab
	KeyCtrlC
	KeyOther
)

type Key struct {
	Kind KeyKind
	Rune rune
}

type KeySource func() ([]Key, error)

func StdinKeys(f *os.File) KeySource {
	buf := make([]byte, 256)
	return func() ([]Key, error) {
		n, err := f.Read(buf)
		if n > 0 {
			return ParseKeys(buf[:n]), nil
		}
		if err == nil {
			return nil, nil
		}
		return nil, err
	}
}

func ParseKeys(b []byte) []Key {
	var keys []Key
	for len(b) > 0 {
		key, n := parseOne(b)
		keys = append(keys, key)
		b = b[n:]
	}
	return keys
}

func parseOne(b []byte) (Key, int) {
	switch b[0] {
	case 0x1b:
		return parseEscape(b)
	case '\r', '\n':
		return Key{Kind: KeyEnter}, 1
	case 0x7f, 0x08:
		return Key{Kind: KeyBackspace}, 1
	case '\t':
		return Key{Kind: KeyTab}, 1
	case 0x03:
		return Key{Kind: KeyCtrlC}, 1
	}
	if b[0] < 0x20 {
		return Key{Kind: KeyOther}, 1
	}
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size <= 1 {
		return Key{Kind: KeyOther}, 1
	}
	return Key{Kind: KeyRune, Rune: r}, size
}

func parseEscape(b []byte) (Key, int) {
	if len(b) < 2 || (b[1] != '[' && b[1] != 'O') {
		return Key{Kind: KeyEsc}, 1
	}
	for i := 2; i < len(b); i++ {
		if b[i] < 0x40 || b[i] > 0x7e {
			continue
		}
		kind := KeyOther
		if i == 2 {
			switch b[i] {
			case 'A':
				kind = KeyUp
			case 'B':
				kind = KeyDown
			case 'C':
				kind = KeyRight
			case 'D':
				kind = KeyLeft
			}
		}
		return Key{Kind: kind}, i + 1
	}
	return Key{Kind: KeyOther}, len(b)
}
