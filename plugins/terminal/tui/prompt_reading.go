package tui

import contracts "github.com/Herrscherd/herrscher-contracts"

type promptReading struct {
	fresh, cacheRead, cacheCreate int
}

func (p promptReading) total() int {
	return p.fresh + p.cacheRead + p.cacheCreate
}

func (p promptReading) cacheShare() float64 {
	total := p.total()
	if total <= 0 {
		return 0
	}
	return float64(p.cacheRead) / float64(total)
}

func (tb *tab) measureInjected(reply contracts.Event) {
	tb.injected = reply.Injected
	tb.injectedKnown = true
	if reading := (promptReading{fresh: reply.TokensIn, cacheRead: reply.CacheRead, cacheCreate: reply.CacheCreate}); reading.total() > 0 {
		tb.lastPrompt = reading
	}
}
