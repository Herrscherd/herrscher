package host

import (
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
)

const (
	journalDepth         = 512
	journalSessions      = 64
	journalRingBytes     = 1 << 20
	journalChunkBytes    = 64 << 10
	journalChunkMarks    = 1024
	journalEntryOverhead = 128
)

// EventCursor is the first line a subscriber may write on the events socket: the session to catch up on and the last seq it saw, or no seq for the turn in progress.
type EventCursor struct {
	Session string  `json:"session"`
	Since   *uint64 `json:"since,omitempty"`
}

type chunkMark struct {
	seq uint64
	end int
}

type journalEntry struct {
	wireEvent

	marks []chunkMark
}

func (e *journalEntry) cost() int {
	return len(e.Text) + journalEntryOverhead
}

func (e *journalEntry) between(after, upTo uint64) (wireEvent, bool) {
	if len(e.marks) == 0 {
		return e.wireEvent, e.Seq > after && e.Seq <= upTo
	}
	shown := e.wireEvent
	seenEnd, shownEnd, held := 0, 0, false
	for _, mark := range e.marks {
		if mark.seq <= after {
			seenEnd = mark.end
		}
		if mark.seq > after && mark.seq <= upTo {
			shownEnd, shown.Seq, held = mark.end, mark.seq, true
		}
	}
	if !held {
		return wireEvent{}, false
	}
	shown.Text = e.Text[seenEnd:shownEnd]
	return shown, true
}

func (e *journalEntry) absorbs(next contracts.Event) bool {
	return e.T == eventwire.EventChunk && next.T == eventwire.EventChunk &&
		e.Who == next.Who && e.TurnID == next.TurnID &&
		len(e.Text)+len(next.Text) <= journalChunkBytes && len(e.marks) < journalChunkMarks
}

type journalRing struct {
	entries []journalEntry
	bytes   int
	floor   uint64
}

func (r *journalRing) add(stamped wireEvent) {
	if last := len(r.entries) - 1; last >= 0 && r.entries[last].absorbs(stamped.Event) {
		entry := &r.entries[last]
		entry.Text += stamped.Text
		entry.Seq = stamped.Seq
		entry.marks = append(entry.marks, chunkMark{seq: stamped.Seq, end: len(entry.Text)})
		r.bytes += len(stamped.Text)
	} else {
		entry := journalEntry{wireEvent: stamped}
		if stamped.T == eventwire.EventChunk {
			entry.marks = []chunkMark{{seq: stamped.Seq, end: len(stamped.Text)}}
		}
		r.entries = append(r.entries, entry)
		r.bytes += entry.cost()
	}
	r.trim()
}

func (r *journalRing) trim() {
	dropped := 0
	for len(r.entries)-dropped > 1 && (len(r.entries)-dropped > journalDepth || r.bytes > journalRingBytes) {
		r.bytes -= r.entries[dropped].cost()
		r.floor = r.entries[dropped].Seq
		dropped++
	}
	if dropped > 0 {
		r.entries = append(r.entries[:0:0], r.entries[dropped:]...)
	}
}

func (r *journalRing) lastSeq() uint64 {
	if len(r.entries) == 0 {
		return r.floor
	}
	return r.entries[len(r.entries)-1].Seq
}

func (r *journalRing) between(after, upTo uint64) []wireEvent {
	var events []wireEvent
	for idx := range r.entries {
		if shown, held := r.entries[idx].between(after, upTo); held {
			events = append(events, shown)
		}
	}
	return events
}

func (r *journalRing) turn(upTo uint64) []wireEvent {
	start := 0
	for idx := range r.entries {
		if r.entries[idx].T == eventwire.EventHuman && r.entries[idx].Seq <= upTo {
			start = idx
		}
	}
	held := journalRing{entries: r.entries[start:]}
	return held.between(0, upTo)
}

func turnState(events []wireEvent) string {
	if len(events) == 0 {
		return eventwire.CaughtUpEmpty
	}
	last := events[len(events)-1]
	if last.T == eventwire.EventAbandoned || last.T == eventwire.EventReply && last.Done {
		return eventwire.CaughtUpIdle
	}
	return eventwire.CaughtUpRunning
}

type eventJournal struct {
	mu    sync.Mutex
	seq   uint64
	floor uint64
	rings map[string]*journalRing
}

func newEventJournal() *eventJournal {
	return newEventJournalFrom(uint64(time.Now().UnixMicro()))
}

func newEventJournalFrom(base uint64) *eventJournal {
	return &eventJournal{seq: base, floor: base, rings: make(map[string]*journalRing)}
}

func (j *eventJournal) record(session string, event contracts.Event) wireEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	stamped := wireEvent{Event: event, Session: session, Seq: j.seq}
	ring, known := j.rings[session]
	if !known {
		if len(j.rings) >= journalSessions {
			j.evictQuietest()
		}
		ring = &journalRing{floor: j.floor}
		j.rings[session] = ring
	}
	ring.add(stamped)
	return stamped
}

func (j *eventJournal) evictQuietest() {
	quietest, oldest := "", j.seq+1
	for session, ring := range j.rings {
		if last := ring.lastSeq(); last < oldest {
			quietest, oldest = session, last
		}
	}
	j.floor = max(j.floor, oldest)
	delete(j.rings, quietest)
}

func (j *eventJournal) current() uint64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.seq
}

func (j *eventJournal) catchUp(cursor EventCursor, upTo uint64) []wireEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	ring, known := j.rings[cursor.Session]
	if !known {
		ring = &journalRing{floor: j.floor}
	}
	var replay []wireEvent
	var events []wireEvent
	if cursor.Since == nil {
		events = ring.turn(upTo)
	} else {
		after := *cursor.Since
		if after > j.seq || after < ring.floor {
			replay = append(replay, wireEvent{Event: contracts.Event{T: eventwire.EventGap, Value: eventwire.GapExpired}, Session: cursor.Session})
		}
		if after > j.seq {
			after = 0
		}
		events = ring.between(after, upTo)
	}
	replay = append(replay, events...)
	caughtUp := contracts.Event{T: eventwire.EventCaughtUp, Value: turnState(ring.between(0, upTo))}
	return append(replay, wireEvent{Event: caughtUp, Session: cursor.Session})
}

func (j *eventJournal) rename(from, target string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	ring, known := j.rings[from]
	if !known {
		return
	}
	delete(j.rings, from)
	for idx := range ring.entries {
		ring.entries[idx].Session = target
	}
	j.rings[target] = ring
}

func (h *hub) eventJournal() *eventJournal {
	if h.sessions == nil || h.sessions.journal == nil {
		return newEventJournal()
	}
	return h.sessions.journal
}
