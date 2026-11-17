package host

import (
	"fmt"
	"io"
	"log/slog"
	"sync"
)

const stderrQueueLimit = 32

type stderrLine struct {
	out     io.Writer
	text    string
	written chan struct{}
}

type stderrPort struct {
	mu      sync.Mutex
	pending []stderrLine
	running chan struct{}
	halted  bool
	dropped int
	log     boundedLog
}

func (p *stderrPort) offer(out io.Writer, text string) <-chan struct{} {
	written := make(chan struct{})
	p.mu.Lock()
	if p.halted || len(p.pending) >= stderrQueueLimit {
		first := p.dropped == 0
		p.dropped++
		p.mu.Unlock()
		close(written)
		if first {
			p.log.warn(func() { slog.Warn("stderr line dropped, the port is full or closed") })
		}
		return written
	}
	p.pending = append(p.pending, stderrLine{out: out, text: text, written: written})
	if p.running == nil {
		p.running = make(chan struct{})
		go p.drain()
	}
	p.mu.Unlock()
	return written
}

func (p *stderrPort) drain() {
	for {
		line, ok := p.next()
		if !ok {
			return
		}
		p.emit(line)
	}
}

func (p *stderrPort) next() (stderrLine, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pending) == 0 {
		close(p.running)
		p.running = nil
		return stderrLine{}, false
	}
	line := p.pending[0]
	p.pending = p.pending[1:]
	return line, true
}

func (p *stderrPort) emit(line stderrLine) {
	p.mu.Lock()
	if p.halted {
		p.dropped++
		p.mu.Unlock()
		close(line.written)
		return
	}
	p.mu.Unlock()
	logWriteFailure(fmt.Fprintln(line.out, line.text))
	close(line.written)
}

func (p *stderrPort) shutdown(budget <-chan struct{}) bool {
	p.mu.Lock()
	if !p.halted {
		p.halted = true
		p.dropped += len(p.pending)
		for _, line := range p.pending {
			close(line.written)
		}
		p.pending = nil
	}
	running := p.running
	p.mu.Unlock()
	if running == nil {
		return true
	}
	select {
	case <-running:
		return true
	case <-budget:
		return false
	}
}

func (p *stderrPort) isHalted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.halted
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
		note := newHostMessages().text(h.textLanguage()(), "warn.stderr_abandoned", nil)
		h.errPort.log.reserve(func() {
			slog.Warn("stderr lines were abandoned at shutdown", "note", note, "dropped", dropped, "writer_joined", joined)
		})
		logged, stopLogged := writeBudget()
		defer stopLogged()
		h.errPort.log.drain(logged)
	}
}
