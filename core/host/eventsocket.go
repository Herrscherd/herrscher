package host

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
)

type wireEvent struct {
	contracts.Event

	Session string `json:"session"`
	Seq     uint64 `json:"seq,omitempty"`
}

func marshalSessionEvent(session string, event contracts.Event) ([]byte, error) {
	return marshalWireEvent(wireEvent{Session: session, Event: event})
}

func marshalWireEvent(stamped wireEvent) ([]byte, error) {
	payload, err := json.Marshal(stamped)
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

const subscriberBuffer = 256

const publishWriteTimeout = 2 * time.Second

const (
	cursorWait  = 200 * time.Millisecond
	cursorBytes = 1024
)

type subscriber struct {
	conn       net.Conn
	ch         chan []byte
	priorityMu sync.Mutex
	priority   [][]byte
	dropped    atomic.Bool
}

type eventSocket struct {
	mu      sync.Mutex
	subs    map[*subscriber]struct{}
	closed  bool
	journal *eventJournal
}

func newEventSocket() *eventSocket {
	return newJournaledEventSocket(newEventJournal())
}

func newJournaledEventSocket(journal *eventJournal) *eventSocket {
	return &eventSocket{subs: make(map[*subscriber]struct{}), journal: journal}
}

func (s *eventSocket) add(conn net.Conn) {
	sub := &subscriber{conn: conn, ch: make(chan []byte, subscriberBuffer)}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		logCleanup(conn.Close())
		return
	}
	s.subs[sub] = struct{}{}
	upTo := s.journal.current()
	s.mu.Unlock()
	go s.serve(sub, upTo)
}

func (s *eventSocket) serve(sub *subscriber, upTo uint64) {
	defer func() {
		s.remove(sub)
		logCleanup(sub.conn.Close())
	}()
	if !s.catchUp(sub, upTo) {
		return
	}
	for {
		line, ok := sub.next()
		if !ok {
			return
		}
		if sub.dropped.Swap(false) && !sub.write(wireEvent{Event: contracts.Event{T: eventwire.EventGap, Value: eventwire.GapDropped}}) {
			return
		}
		if !sub.send(line) {
			return
		}
	}
}

type cursorOutcome int

const (
	cursorAbsent cursorOutcome = iota
	cursorGiven
	cursorUnreadable
)

func (s *eventSocket) catchUp(sub *subscriber, upTo uint64) bool {
	cursor, outcome := readCursor(sub.conn)
	switch outcome {
	case cursorAbsent:
		return true
	case cursorUnreadable:
		return sub.write(wireEvent{Event: contracts.Event{T: eventwire.EventGap, Value: eventwire.GapDropped}})
	case cursorGiven:
		return s.replay(sub, cursor, upTo)
	}
	return true
}

func (s *eventSocket) replay(sub *subscriber, cursor EventCursor, upTo uint64) bool {
	held := s.journal.catchUp(cursor, upTo)
	for idx := range held {
		if !sub.write(held[idx]) {
			return false
		}
	}
	return true
}

func readCursor(conn net.Conn) (EventCursor, cursorOutcome) {
	var cursor EventCursor
	if conn.SetReadDeadline(time.Now().Add(cursorWait)) != nil {
		return cursor, cursorAbsent
	}
	line, err := bufio.NewReader(io.LimitReader(conn, cursorBytes)).ReadBytes('\n')
	logCleanup(conn.SetReadDeadline(time.Time{}))
	switch {
	case len(line) == 0:
		return cursor, cursorAbsent
	case err != nil || json.Unmarshal(line, &cursor) != nil:
		return cursor, cursorUnreadable
	case cursor.Session == "":
		return cursor, cursorAbsent
	}
	return cursor, cursorGiven
}

func (sub *subscriber) write(event wireEvent) bool {
	line, err := marshalWireEvent(event)
	return err == nil && sub.send(line)
}

func (sub *subscriber) send(line []byte) bool {
	logCleanup(sub.conn.SetWriteDeadline(time.Now().Add(publishWriteTimeout)))
	_, err := sub.conn.Write(line)
	return err == nil
}

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

func (s *eventSocket) remove(sub *subscriber) {
	s.mu.Lock()
	if _, ok := s.subs[sub]; ok {
		delete(s.subs, sub)
		close(sub.ch)
	}
	s.mu.Unlock()
}

func (s *eventSocket) Publish(session string, event contracts.Event) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stamped := s.journal.record(session, event)
	if len(s.subs) == 0 {
		return
	}
	line, err := marshalWireEvent(stamped)
	if err != nil {
		return
	}
	for sub := range s.subs {
		if event.Coordination != nil {
			sub.enqueuePriority(line)
			continue
		}
		select {
		case sub.ch <- line:
		default:
			sub.dropped.Store(true)
		}
	}
}

func (s *eventSocket) closeAll() {
	s.mu.Lock()
	s.closed = true
	for sub := range s.subs {
		delete(s.subs, sub)
		close(sub.ch)
	}
	s.mu.Unlock()
}

func acceptEvents(ctx context.Context, listener net.Listener, socket *eventSocket) <-chan struct{} {
	released := make(chan struct{})
	go func() {
		<-ctx.Done()
		closeRead(listener)
		socket.closeAll()
		close(released)
	}()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			socket.add(conn)
		}
	}()
	return released
}
