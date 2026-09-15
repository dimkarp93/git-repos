package render

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestSpinnerDrawsLabelAndClearsOnStop(t *testing.T) {
	out := &syncBuffer{}
	s := NewSpinner(out, true)
	counter := 0
	s.Start(func() string {
		counter++
		return "в работе — 3"
	})
	time.Sleep(3 * spinnerInterval)
	s.Stop()

	got := out.String()
	if !strings.Contains(got, "в работе — 3") {
		t.Fatalf("label missing: %q", got)
	}
	if !strings.HasSuffix(got, "\r\x1b[K") {
		t.Fatalf("line not cleared on stop: %q", got)
	}
	if counter < 2 {
		t.Fatalf("label evaluated %d times, expected repeated redraws", counter)
	}
	frames := 0
	for _, frame := range spinnerFrames {
		if strings.Contains(got, frame) {
			frames++
		}
	}
	if frames < 2 {
		t.Fatalf("spinner did not animate: %q", got)
	}
}

func TestSpinnerDisabledWritesNothing(t *testing.T) {
	out := &syncBuffer{}
	s := NewSpinner(out, false)
	s.Start(func() string { return "x" })
	time.Sleep(2 * spinnerInterval)
	s.Stop()
	if out.String() != "" {
		t.Fatalf("disabled spinner wrote %q", out.String())
	}
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
	if !strings.Contains(got, "первый") || !strings.Contains(got, "второй") {
		t.Fatalf("output = %q", got)
	}
}
