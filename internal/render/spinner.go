package render

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const spinnerInterval = 100 * time.Millisecond

type Spinner struct {
	out     io.Writer
	enabled bool
	label   func() string
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	width   int
}

func NewSpinner(out io.Writer, enabled bool) *Spinner {
	return &Spinner{out: out, enabled: enabled}
}

func IsTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (s *Spinner) Start(label func() string) {
	if !s.enabled || label == nil || s.stop != nil {
		return
	}
	s.label = label
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.loop()
}

func (s *Spinner) loop() {
	defer close(s.done)
	ticker := time.NewTicker(spinnerInterval)
	defer ticker.Stop()
	frame := 0
	s.draw(spinnerFrames[0])
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			frame = (frame + 1) % len(spinnerFrames)
			s.draw(spinnerFrames[frame])
		}
	}
}

func (s *Spinner) draw(frame string) {
	text := s.label()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.width = len(text) + 2
	fmt.Fprintf(s.out, "\r\x1b[K%s %s", frame, text)
}

func (s *Spinner) Stop() {
	if s.stop == nil {
		return
	}
	close(s.stop)
	<-s.done
	s.stop = nil
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprint(s.out, "\r\x1b[K")
}
