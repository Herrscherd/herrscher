package prwatch

import (
	"sync"
	"time"
)

// Limit is the forge rate limit shared by the watches of one host: none of them reads
// the forge before the time it holds until.
type Limit struct {
	mu    sync.Mutex
	until time.Time
}

// Hold stops every watch sharing the limit from reading the forge before until.
func (l *Limit) Hold(until time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if until.After(l.until) {
		l.until = until
	}
}

// Until returns the time before which no watch sharing the limit reads the forge.
func (l *Limit) Until() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.until
}
