package render

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type countedLabel struct {
	calls int
}

func (c *countedLabel) label() string {
	c.calls++
	return "in flight — 3"
}

func spun(t *testing.T) (string, int) {
	t.Helper()

	out := &syncBuffer{}
	s := NewSpinner(out, true)
	label := &countedLabel{}
	s.Start(label.label)
	time.Sleep(3 * spinnerInterval)
	s.Stop()

	return out.String(), label.calls
}

func animationFrames(out string) int {
	frames := 0
	for _, frame := range spinnerFrames {
		if strings.Contains(out, frame) {
			frames++
		}
	}
	return frames
}

func TestSpinnerDrawsLabelAndClearsOnStop(t *testing.T) {
	got, calls := spun(t)

	t.Run("label", func(t *testing.T) {
		assert.Contains(t, got, "in flight — 3")
	})
	t.Run("cleared on stop", func(t *testing.T) {
		assert.True(t, strings.HasSuffix(got, "\r\x1b[K"), got)
	})
	t.Run("redrawn", func(t *testing.T) {
		assert.GreaterOrEqual(t, calls, 2)
	})
	t.Run("animated", func(t *testing.T) {
		assert.GreaterOrEqual(t, animationFrames(got), 2)
	})
}

func TestSpinnerDisabledWritesNothing(t *testing.T) {
	out := &syncBuffer{}
	s := NewSpinner(out, false)
	s.Start(func() string { return "x" })
	time.Sleep(2 * spinnerInterval)
	s.Stop()
	require.Equal(t, "", out.String(), "disabled spinner wrote %q", out.String())
}

func TestSpinnerRestartAndDoubleStop(t *testing.T) {
	out := &syncBuffer{}
	s := NewSpinner(out, true)
	s.Start(func() string { return "первый" })
	time.Sleep(spinnerInterval)
	s.Stop()
	s.Stop()

	s.Start(func() string { return "второй" })
	time.Sleep(spinnerInterval)
	s.Stop()

	got := out.String()
	assert.Contains(t, got, "первый")
	assert.Contains(t, got, "второй")
}
