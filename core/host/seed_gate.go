package host

import (
	"io"
	"sync"
)

const (
	lateTranscriptKey = "seed.late_transcript"
	lateEventKey      = "seed.late_event"
	lateUsageKey      = "seed.late_usage"
	lateResumeKey     = "seed.late_resume"
)

type seedGate struct {
	mu       sync.Mutex
	closed   bool
	refused  map[string]bool
	session  string
	warn     io.Writer
	language languageSource
}

func newSeedGate(session string, warn io.Writer, language languageSource) *seedGate {
	return &seedGate{refused: make(map[string]bool), session: session, warn: warn, language: language}
}

func (g *seedGate) admit(lateKey string, deliver func()) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		deliver()
		return
	}
	if !g.refused[lateKey] {
		g.refused[lateKey] = true
		warnLive(g.warn, g.language, lateKey, map[string]any{nameParam: g.session})
	}
}

func (g *seedGate) shut() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
}
