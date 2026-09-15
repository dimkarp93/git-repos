package render

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestAgeColorThresholds(t *testing.T) {
	day := 24 * time.Hour
	cases := []struct {
		d    time.Duration
		want string
	}{
		{time.Hour, ""},
		{2 * day, ""},
		{3 * day, Yellow},
		{9 * day, Yellow},
		{10 * day, Orange},
		{29 * day, Orange},
		{30 * day, Red},
		{100 * day, Red},
	}
	for _, tc := range cases {
		if got := AgeColor(tc.d); got != tc.want {
			t.Errorf("AgeColor(%s) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFetchAgeNever(t *testing.T) {
	text, color := FetchAge(time.Time{}, false)
	if text != "never" || color != Red {
		t.Fatalf("FetchAge = %q, %q", text, color)
	}
	text, _ = FetchAge(time.Now().Add(-50*time.Hour), true)
	if text != "2d ago" {
		t.Fatalf("FetchAge = %q", text)
	}
}

func TestTableAlignsColumnsWithoutColor(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf, false)
	p.Table("", []string{"A", "B"}, [][]Cell{
		{{Text: "long-value"}, {Text: "x"}},
		{{Text: "s"}, {Text: "y"}},
	})
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("lines = %q", lines)
	}
	if lines[0] != "┌────────────┬───┐" || lines[1] != "│ A          │ B │" {
		t.Fatalf("lines = %q", lines)
	}
	if lines[3] != "│ long-value │ x │" || lines[4] != "│ s          │ y │" {
		t.Fatalf("lines = %q", lines)
	}
	if lines[5] != "└────────────┴───┘" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestTableColorsCells(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf, true)
	p.Table("T", []string{"A"}, [][]Cell{{{Text: "bad", Color: Red}}})
	if !strings.Contains(buf.String(), Red+"bad"+Reset) {
		t.Fatalf("output = %q", buf.String())
	}
}
