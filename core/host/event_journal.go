package host

import (
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
)

const (
	journalDepth    = 512
	journalSessions = 64
)

type chunkMark struct {
	seq uint64
	end int
}

type journalEntry struct {
	wireEvent

	marks []chunkMark
}

type eventJournal struct {
	mu    sync.Mutex
	seq   uint64
	rings map[string][]journalEntry
}

func newEventJournal() *eventJournal {
	return &eventJournal{rings: make(map[string][]journalEntry)}
}

func (j *eventJournal) record(session string, event contracts.Event) wireEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	stamped := wireEvent{Event: event, Session: session, Seq: j.seq}
	if _, known := j.rings[session]; !known && len(j.rings) >= journalSessions {
		j.evictQuietest()
	}
	ring := j.rings[session]
	if last := len(ring) - 1; last >= 0 && continuesChunk(ring[last].Event, event) {
		ring[last].Text += event.Text
		ring[last].Seq = j.seq
		ring[last].marks = append(ring[last].marks, chunkMark{seq: j.seq, end: len(ring[last].Text)})
		return stamped
	}
	entry := journalEntry{wireEvent: stamped}
	if event.T == eventwire.EventChunk {
		entry.marks = []chunkMark{{seq: j.seq, end: len(event.Text)}}
	}
	ring = append(ring, entry)
	if len(ring) > journalDepth {
		ring = append(ring[:0:0], ring[len(ring)-journalDepth:]...)
	}
	j.rings[session] = ring
	return stamped
}

func continuesChunk(last, next contracts.Event) bool {
	return last.T == eventwire.EventChunk && next.T == eventwire.EventChunk && last.Who == next.Who && last.TurnID == next.TurnID
}

func (j *eventJournal) evictQuietest() {
	quietest, oldest := "", j.seq+1
	for session, ring := range j.rings {
		if last := ring[len(ring)-1].Seq; last < oldest {
			quietest, oldest = session, last
		}
	}
	delete(j.rings, quietest)
}

func (j *eventJournal) since(session string, after uint64) []wireEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	ring := j.rings[session]
	if after > j.seq {
		after = 0
	}
	start := len(ring)
	for start > 0 && ring[start-1].Seq > after {
		start--
	}
	events := unstamp(ring[start:])
	if len(events) > 0 {
		events[0].Text = unseenText(ring[start], after)
	}
	return events
}

func unseenText(entry journalEntry, after uint64) string {
	seen := 0
	for _, mark := range entry.marks {
		if mark.seq <= after {
			seen = mark.end
		}
	}
	return entry.Text[seen:]
}

func (j *eventJournal) currentTurn(session string) []wireEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	ring := j.rings[session]
	start := len(ring)
	for start > 0 {
		start--
		if ring[start].T == eventwire.EventHuman {
			break
		}
	}
	return unstamp(ring[start:])
}

func unstamp(entries []journalEntry) []wireEvent {
	events := make([]wireEvent, len(entries))
	for idx := range entries {
		events[idx] = entries[idx].wireEvent
	}
	return events
}

func (h *hub) eventJournal() *eventJournal {
	if h.sessions == nil || h.sessions.journal == nil {
		return newEventJournal()
	}
	return h.sessions.journal
}
