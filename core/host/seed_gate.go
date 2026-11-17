package host

import "sync"

type seedGate struct {
	mu     sync.Mutex
	closed bool
}

func (g *seedGate) admit(deliver func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		deliver()
	}
}

func (g *seedGate) shut() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
}
