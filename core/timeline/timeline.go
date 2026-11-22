// Package timeline rebuilds the tool calls of one turn from the events that carry them, the same way for the host, the transcript, the watch and the terminal.
package timeline

import (
	"errors"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

// MaxCalls bounds the calls one turn keeps. A call that starts past it is not kept, the turn's text and tokens are unchanged, and Apply reports ErrFull once.
const MaxCalls = 4096

// ErrFull is returned by Apply once, on the first call a turn drops for MaxCalls.
var ErrFull = errors.New("timeline: the turn has more tool calls than it keeps")

// Call is one tool call of a turn. A call still open at the end of its turn has
// no EndedAt, no DurationMs and no outcome beyond its started state.
type Call struct {
	ID         string                  `json:"id"`
	Tool       string                  `json:"tool"`
	Detail     string                  `json:"detail,omitempty"`
	State      contracts.ToolCallState `json:"state"`
	StartedAt  time.Time               `json:"started_at"`
	EndedAt    *time.Time              `json:"ended_at,omitempty"`
	DurationMs *int64                  `json:"duration_ms,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Tokens     int                     `json:"tokens"`
}

// Finished reports whether the call has a result.
func (c Call) Finished() bool {
	return c.EndedAt != nil
}

// Label is the call as one line: its tool, then its detail when it has one.
func (c Call) Label() string {
	return strings.TrimSpace(c.Tool + " " + c.Detail)
}

// Recorder keeps the tool calls of one turn. Its zero value is ready to use, and
// it is fed the turn's events in order. A reset starts a new execution: the calls
// still open are kept as unfinished, and the execution forgets their IDs.
type Recorder struct {
	calls      []Call
	openIndex  map[string]int
	started    map[string]bool
	cumulative int
	mark       int
	marked     bool
	full       bool
}

// Apply feeds one event of the turn to the recorder. It returns a copy of the
// call the event started or finished, whether the event touched a call, and
// ErrFull on the first call the turn cannot keep.
func (r *Recorder) Apply(event contracts.Event) (Call, bool, error) {
	switch {
	case event.T == eventwire.EventReset:
		r.reset()
		return Call{}, false, nil
	case event.ToolCall != nil && event.T == eventwire.EventStatus && event.ToolCall.State == contracts.ToolCallStarted:
		return r.open(event)
	case event.ToolCall != nil && event.T == eventwire.EventToolDone && event.ToolCall.State.Finished():
		call, touched := r.close(event)
		return call, touched, nil
	}
	if carriesTokens(event.T) {
		r.cumulative = event.Tokens
	}
	return Call{}, false, nil
}

// Calls returns the calls of the turn in the order they started, open ones included.
func (r *Recorder) Calls() []Call {
	return slices.Clone(r.calls)
}

func (r *Recorder) reset() {
	r.openIndex = nil
	r.started = nil
}

func (r *Recorder) open(event contracts.Event) (Call, bool, error) {
	call := event.ToolCall
	if r.started[call.ID] {
		return Call{}, false, nil
	}
	if len(r.calls) >= MaxCalls {
		if r.full {
			return Call{}, false, nil
		}
		r.full = true
		return Call{}, false, ErrFull
	}
	if r.started == nil {
		r.started = make(map[string]bool)
		r.openIndex = make(map[string]int)
	}
	r.started[call.ID] = true
	r.cumulative = event.Tokens
	entry := Call{ID: call.ID, Tool: call.Tool, Detail: call.Detail, State: contracts.ToolCallStarted, StartedAt: call.At, Tokens: r.tokensSinceMark()}
	r.setMark()
	r.calls = append(r.calls, entry)
	r.openIndex[call.ID] = len(r.calls) - 1
	return entry, true, nil
}

func (r *Recorder) close(event contracts.Event) (Call, bool) {
	call := event.ToolCall
	index, open := r.openIndex[call.ID]
	if !open {
		return Call{}, false
	}
	r.cumulative = event.Tokens
	entry := &r.calls[index]
	ended := call.At
	elapsed := ended.Sub(entry.StartedAt).Milliseconds()
	entry.State = call.State
	entry.Error = call.Error
	entry.EndedAt = &ended
	entry.DurationMs = &elapsed
	delete(r.openIndex, call.ID)
	r.setMark()
	return *entry, true
}

func (r *Recorder) tokensSinceMark() int {
	if !r.marked {
		return 0
	}
	return r.cumulative - r.mark
}

func (r *Recorder) setMark() {
	r.mark = r.cumulative
	r.marked = true
}

func carriesTokens(eventType string) bool {
	switch eventType {
	case eventwire.EventChunk, eventwire.EventThinking, eventwire.EventStatus, eventwire.EventToolDone, eventwire.EventReply:
		return true
	}
	return false
}
