package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		assert.Equal(t, tc.want, AgeColor(tc.d))
	}
}

func TestFetchAgeNever(t *testing.T) {
	text, color := FetchAge(time.Time{}, false)

	assert.Equal(t, []string{"never", Red}, []string{text, color})
}

func TestFetchAgeKnown(t *testing.T) {
	text, _ := FetchAge(time.Now().Add(-50*time.Hour), true)

	assert.Equal(t, "2d ago", text)
}

func TestTableAlignsColumnsWithoutColor(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf, false)
	p.Table("", []string{"A", "B"}, [][]Cell{
		{{Text: "long-value"}, {Text: "x"}},
		{{Text: "s"}, {Text: "y"}},
	})

	want := []string{
		"┌────────────┬───┐",
		"│ A          │ B │",
		"├────────────┼───┤",
		"│ long-value │ x │",
		"│ s          │ y │",
		"└────────────┴───┘",
	}
	assert.Equal(t, want, strings.Split(strings.TrimRight(buf.String(), "\n"), "\n"))
}

func TestTableColorsCells(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf, true)
	p.Table("T", []string{"A"}, [][]Cell{{{Text: "bad", Color: Red}}})
	require.Contains(t, buf.String(), Red+"bad"+Reset, "output = %q", buf.String())
}
