package host

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type wireEvent struct {
	contracts.Event

	Session string `json:"session"`
}

func marshalSessionEvent(session string, event contracts.Event) ([]byte, error) {
	payload, err := json.Marshal(wireEvent{
		Session: session,
		Event:   event,
	})
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

const subscriberBuffer = 256

const publishWriteTimeout = 2 * time.Second

type subscriber struct {
	conn       net.Conn
	ch         chan []byte
	priorityMu sync.Mutex
	priority   [][]byte
}

type eventSocket struct {
	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	closed bool
}

func newEventSocket() *eventSocket {
	return &eventSocket{subs: make(map[*subscriber]struct{})}
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
	s.mu.Unlock()
	go s.serve(sub)
}

func (s *eventSocket) serve(sub *subscriber) {
	defer func() {
		s.remove(sub)
		logCleanup(sub.conn.Close())
	}()
	for {
		line, ok := sub.next()
		if !ok {
			return
		}
		logCleanup(sub.conn.SetWriteDeadline(time.Now().Add(publishWriteTimeout)))
		if _, err := sub.conn.Write(line); err != nil {
			return
		}
	}
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
	idle := len(s.subs) == 0
	s.mu.Unlock()
	if idle {
		return
	}
	line, err := marshalSessionEvent(session, event)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subs {
		if event.Coordination != nil {
			sub.enqueuePriority(line)
			continue
		}
		select {
		case sub.ch <- line:
		default:
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

func openEventsSocket(ctx context.Context, path string) (ownedListener, error) {
	if err := claimSocketPath(ctx, path); err != nil {
		return ownedListener{}, err
	}
	return listenOwned(ctx, path)
}

func acceptEvents(ctx context.Context, listener ownedListener, socket *eventSocket) <-chan struct{} {
	released := make(chan struct{})
	go func() {
		<-ctx.Done()
		listener.release()
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
