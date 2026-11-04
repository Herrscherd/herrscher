package host

import (
	"sync"
	"time"
)

type seenTable struct {
	mu    sync.Mutex
	floor time.Time
	at    map[string]time.Time
}

func newSeenTable(floor time.Time) *seenTable {
	return &seenTable{floor: floor, at: make(map[string]time.Time)}
}

func (t *seenTable) mark(name string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.at[name] = now
}

func (t *seenTable) since(name string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if seen, ok := t.at[name]; ok {
		return seen
	}
	return t.floor
}

func needsReview(lastDone string, seenAt time.Time) bool {
	done, err := time.Parse(time.RFC3339, lastDone)
	return err == nil && done.After(seenAt)
}
