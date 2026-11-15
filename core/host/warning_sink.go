package host

import (
	"bytes"
	"io"
	"sync"
)

const warningQueueLimit = 256

type warningSink struct {
	out     io.Writer
	mu      sync.Mutex
	queue   [][]byte
	dropped int
	writing bool
	idle    chan struct{}
}

func newWarningSink(out io.Writer) *warningSink {
	return &warningSink{out: out}
}

func (s *warningSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) >= warningQueueLimit {
		s.dropped++
		return len(p), nil
	}
	s.queue = append(s.queue, bytes.Clone(p))
	if !s.writing {
		s.writing = true
		s.idle = make(chan struct{})
		go s.drain()
	}
	return len(p), nil
}

func (s *warningSink) drain() {
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.writing = false
			close(s.idle)
			s.mu.Unlock()
			return
		}
		next := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		logWriteFailure(warningWriter(s.out).Write(next))
	}
}

func (s *warningSink) flush(window teardownWindow) bool {
	s.mu.Lock()
	if !s.writing {
		s.mu.Unlock()
		return true
	}
	idle := s.idle
	s.mu.Unlock()
	return window.finished(idle)
}

func (s *warningSink) droppedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped
}
