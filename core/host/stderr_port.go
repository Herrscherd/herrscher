package host

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
)

const stderrQueueLimit = 32

type stderrLine struct {
	text    string
	written chan struct{}
}

type stderrPort struct {
	mu      sync.Mutex
	lines   chan stderrLine
	stopped chan struct{}
	halted  bool
	dropped int
}

func (p *stderrPort) offer(out io.Writer, text string) <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	written := make(chan struct{})
	if p.halted {
		p.dropped++
		close(written)
		return written
	}
	if p.lines == nil {
		p.lines = make(chan stderrLine, stderrQueueLimit)
		p.stopped = make(chan struct{})
		go p.run(out, p.lines, p.stopped)
	}
	select {
	case p.lines <- stderrLine{text: text, written: written}:
	default:
		p.dropped++
		close(written)
	}
	return written
}

func (p *stderrPort) run(out io.Writer, lines <-chan stderrLine, stopped chan<- struct{}) {
	defer close(stopped)
	for line := range lines {
		p.emit(out, line.text)
		close(line.written)
	}
}

func (p *stderrPort) emit(out io.Writer, text string) {
	p.mu.Lock()
	if p.halted {
		p.dropped++
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	logWriteFailure(fmt.Fprintln(out, text))
}

func (p *stderrPort) shutdown(budget <-chan struct{}) bool {
	p.mu.Lock()
	if !p.halted {
		p.halted = true
		if p.lines != nil {
			close(p.lines)
		}
	}
	stopped := p.stopped
	p.mu.Unlock()
	if stopped == nil {
		return true
	}
	select {
	case <-stopped:
		return true
	case <-budget:
		return false
	}
}

func (p *stderrPort) droppedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dropped
}

func (h *hub) closeStderr() {
	budget, stop := writeBudget()
	defer stop()
	joined := h.errPort.shutdown(budget)
	if dropped := h.errPort.droppedCount(); dropped > 0 || !joined {
		slog.Warn(newHostMessages().text(h.textLanguage()(), "warn.stderr_abandoned", nil), "dropped", dropped, "writer_joined", joined)
	}
}
