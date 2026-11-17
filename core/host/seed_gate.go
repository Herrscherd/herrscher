package host

import (
	"io"
	"sync"
)

type seedGate struct {
	mu       sync.Mutex
	closed   bool
	refused  bool
	session  string
	warn     io.Writer
	language languageSource
}

func newSeedGate(session string, warn io.Writer, language languageSource) *seedGate {
	return &seedGate{session: session, warn: warn, language: language}
}

func (g *seedGate) admit(deliver func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		deliver()
		return
	}
	if !g.refused {
		g.refused = true
		warnLive(g.warn, g.language, "seed.late_delivery", map[string]any{nameParam: g.session})
	}
}

func (g *seedGate) shut() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
}
