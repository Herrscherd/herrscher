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

// wireEvent is one per-session event line emitted on the events socket. Embedding
// the contract event keeps every current and future event field intact while the
// session tag lets one socket carry every session's stream.
type wireEvent struct {
	Session string `json:"session"`
	contracts.Event
}

// marshalSessionEvent renders one bus event as a newline-terminated JSON line
// tagged with its session id. encoding/json escapes embedded newlines, so one
// event is always exactly one line (matching control.WriteEvent framing).
func marshalSessionEvent(session string, e contracts.Event) ([]byte, error) {
	b, err := json.Marshal(wireEvent{
		Session: session,
		Event:   e,
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// subscriberBuffer bounds a subscriber's pending-line queue. A subscriber that
// falls this far behind is treated as too slow: Publish drops its overflow lines
// (non-blocking send) rather than stalling. Live thinking/chunk streams are
// best-effort telemetry, so a dropped line degrades gracefully.
const subscriberBuffer = 256

// publishWriteTimeout bounds a single socket write in the subscriber's writer
// goroutine. A subscriber whose write trips it is dropped. It runs off the turn
// path (see eventSocket), so this deadline never delays a live turn.
const publishWriteTimeout = 2 * time.Second

// subscriber is one connected reader. Its writer goroutine owns conn: every
// write to that connection goes through this one goroutine draining ch, so
// concurrent Publish calls (from different session turns) never write to the
// same socket at once and can never corrupt the line framing.
type subscriber struct {
	conn       net.Conn
	ch         chan []byte
	priorityMu sync.Mutex
	priority   [][]byte
}

// eventSocket is the daemon-level append-only fan-out socket: every subscriber
// that connects receives JSON event lines. Publish is NON-BLOCKING and never
// touches the network itself. Ordinary telemetry uses a bounded best-effort
// queue, while authoritative coordination frames use a reliable priority queue.
// A stalled reader therefore cannot delay the driver turn or another session,
// and progress pressure cannot discard the terminal coordination outcome. A nil
// *eventSocket's Publish is a no-op, so the CLI/seed path with no socket keeps
// its behaviour.
type eventSocket struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

func newEventSocket() *eventSocket {
	return &eventSocket{subs: map[*subscriber]struct{}{}}
}

// add registers an accepted connection and starts its writer goroutine.
func (s *eventSocket) add(c net.Conn) {
	sub := &subscriber{conn: c, ch: make(chan []byte, subscriberBuffer)}
	s.mu.Lock()
	s.subs[sub] = struct{}{}
	s.mu.Unlock()
	go s.serve(sub)
}

// serve drains one subscriber's queue to its connection until the channel is
// closed (teardown) or a write fails, then deregisters and closes the conn. It
// is the sole writer of sub.conn.
func (s *eventSocket) serve(sub *subscriber) {
	defer func() {
		s.remove(sub)
		_ = sub.conn.Close()
	}()
	for {
		line, ok := sub.next()
		if !ok {
			return
		}
		_ = sub.conn.SetWriteDeadline(time.Now().Add(publishWriteTimeout))
		if _, err := sub.conn.Write(line); err != nil {
			return
		}
	}
}

// enqueuePriority retains an authoritative line outside the bounded telemetry
// queue, then wakes the subscriber writer. When telemetry already fills ch, one
// stale telemetry line is evicted to make room for the wake-up; priority lines
// themselves are never discarded by queue pressure.
func (sub *subscriber) enqueuePriority(line []byte) {
	sub.priorityMu.Lock()
	sub.priority = append(sub.priority, line)
	sub.priorityMu.Unlock()

	select {
	case sub.ch <- nil:
	default:
		select {
		case <-sub.ch:
		default:
		}
		sub.ch <- nil
	}
}

// next returns authoritative lines before ordinary telemetry. A nil telemetry
// entry is only a wake-up inserted by enqueuePriority.
func (sub *subscriber) next() ([]byte, bool) {
	for {
		sub.priorityMu.Lock()
		if len(sub.priority) > 0 {
			line := sub.priority[0]
			sub.priority[0] = nil
			sub.priority = sub.priority[1:]
			sub.priorityMu.Unlock()
			return line, true
		}
		sub.priorityMu.Unlock()

		line, ok := <-sub.ch
		if !ok {
			return nil, false
		}
		if line != nil {
			return line, true
		}
	}
}

// remove deregisters a subscriber and closes its queue exactly once. Guarded by
// the membership check so the writer goroutine's teardown and closeAll cannot
// double-close.
func (s *eventSocket) remove(sub *subscriber) {
	s.mu.Lock()
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
		close(sub.ch)
	}
	s.mu.Unlock()
}

// Publish hands the event to every subscriber without blocking. Authoritative
// coordination frames use a priority queue and are retained under telemetry
// pressure; ordinary events remain best-effort and are dropped when the bounded
// telemetry queue is full. Safe to call on a nil receiver. Never performs I/O.
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
	for sub := range s.subs {
		if e.Coordination != nil {
			sub.enqueuePriority(line)
			continue
		}
		select {
		case sub.ch <- line:
		default:
			// Subscriber's queue is full: too slow, drop this line for it.
		}
	}
}

// closeAll closes every subscriber's queue, ending their writer goroutines
// (which then close their conns). Called on shutdown so no accepted connection
// or goroutine leaks past ctx cancellation.
func (s *eventSocket) closeAll() {
	s.mu.Lock()
	for sub := range s.subs {
		delete(s.subs, sub)
		close(sub.ch)
	}
	s.mu.Unlock()
}

// serveEventsSocket listens on path and registers every accepted connection as a
// subscriber until ctx is done. Mirrors serveCommandSocket's listen/cleanup, but
// the connection is write-only fan-out (no request/response): readers just drain
// the JSON lines. On ctx cancellation it closes the listener and every live
// subscriber so nothing leaks.
func serveEventsSocket(ctx context.Context, path string, s *eventSocket) {
	_ = os.Remove(path)
	ln, err := listenCommandSocket(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "events socket: listen %s: %v\n", path, err)
		return
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
		_ = os.Remove(path)
		s.closeAll()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return // listener closed (ctx done)
		}
		s.add(c)
	}
}
