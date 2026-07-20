package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// wireEvent is one per-session event line emitted on the events socket. Its JSON
// shape is the contract consumed by Neublox's HerrscherEventSource
// (session,t,who,text,value,done,cost) — the session tag lets a single socket
// carry every session's stream, one JSON object per line.
type wireEvent struct {
	Session string  `json:"session"`
	T       string  `json:"t"`
	Who     string  `json:"who,omitempty"`
	Text    string  `json:"text,omitempty"`
	Value   string  `json:"value,omitempty"`
	Done    bool    `json:"done,omitempty"`
	Cost    float64 `json:"cost,omitempty"`
}

// marshalSessionEvent renders one bus event as a newline-terminated JSON line
// tagged with its session id. encoding/json escapes embedded newlines, so one
// event is always exactly one line (matching control.WriteEvent framing).
func marshalSessionEvent(session string, e contracts.Event) ([]byte, error) {
	b, err := json.Marshal(wireEvent{
		Session: session,
		T:       e.T,
		Who:     e.Who,
		Text:    e.Text,
		Value:   e.Value,
		Done:    e.Done,
		Cost:    e.Cost,
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// eventSocket is the daemon-level append-only fan-out socket: every subscriber
// that connects receives one JSON line per published event. Publish is
// best-effort — a subscriber whose write fails or stalls past the deadline is
// dropped so it can never wedge a live turn. A nil *eventSocket's Publish is a
// no-op, so the CLI/seed path with no socket keeps its current behaviour.
type eventSocket struct {
	mu   sync.Mutex
	subs map[net.Conn]struct{}
}

// publishWriteTimeout bounds a single subscriber write so a stuck reader (its
// buffer full) cannot block the driver's fanOut. A subscriber that trips it is
// dropped.
const publishWriteTimeout = 2 * time.Second

func newEventSocket() *eventSocket {
	return &eventSocket{subs: map[net.Conn]struct{}{}}
}

func (s *eventSocket) add(c net.Conn) {
	s.mu.Lock()
	s.subs[c] = struct{}{}
	s.mu.Unlock()
}

// Publish writes the event to every connected subscriber, dropping any whose
// write errors or exceeds publishWriteTimeout. Safe to call on a nil receiver.
func (s *eventSocket) Publish(session string, e contracts.Event) {
	if s == nil {
		return
	}
	line, err := marshalSessionEvent(session, e)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.subs {
		_ = c.SetWriteDeadline(time.Now().Add(publishWriteTimeout))
		if _, err := c.Write(line); err != nil {
			delete(s.subs, c)
			_ = c.Close()
		}
	}
}

// serveEventsSocket listens on path and registers every accepted connection as a
// subscriber until ctx is done. Mirrors serveCommandSocket's listen/cleanup, but
// the connection is write-only fan-out (no request/response): readers just drain
// the JSON lines.
func serveEventsSocket(ctx context.Context, path string, s *eventSocket) {
	_ = os.Remove(path)
	ln, err := listenCommandSocket(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "events socket: listen %s: %v\n", path, err)
		return
	}
	go func() { <-ctx.Done(); _ = ln.Close(); _ = os.Remove(path) }()
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener closed (ctx done)
		}
		s.add(c)
	}
}
