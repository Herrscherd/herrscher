package host

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type wireEvent struct {
	contracts.Event

	Session string `json:"session"`
}

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

const subscriberBuffer = 256

const publishWriteTimeout = 2 * time.Second

type subscriber struct {
	conn       net.Conn
	ch         chan []byte
	priorityMu sync.Mutex
	priority   [][]byte
}

type eventSocket struct {
	mu      sync.Mutex
	subs    map[*subscriber]struct{}
	changed changeNotifier
}

func newEventSocket() *eventSocket {
	return &eventSocket{subs: make(map[*subscriber]struct{})}
}

func (s *eventSocket) add(c net.Conn) {
	sub := &subscriber{conn: c, ch: make(chan []byte, subscriberBuffer)}
	s.mu.Lock()
	s.subs[sub] = struct{}{}
	s.mu.Unlock()
	s.changed.signal()
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

func (s *eventSocket) Publish(session string, e contracts.Event) {
	if s == nil {
		return
	}
	s.mu.Lock()
	idle := len(s.subs) == 0
	s.mu.Unlock()
	if idle {
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
		}
	}
}

func (s *eventSocket) closeAll() {
	s.mu.Lock()
	for sub := range s.subs {
		delete(s.subs, sub)
		close(sub.ch)
	}
	s.mu.Unlock()
}

func serveEventsSocket(ctx context.Context, path string, s *eventSocket) {
	logCleanup(os.Remove(path))
	ln, err := listenCommandSocket(ctx, path)
	if err != nil {
		warnStderr("events socket: listen %s: %v\n", path, err)
		return
	}
	go acceptEvents(ctx, ln, path, s)
}

func acceptEvents(ctx context.Context, ln net.Listener, path string, s *eventSocket) {
	go func() {
		<-ctx.Done()
		logCleanup(ln.Close())
		logCleanup(os.Remove(path))
		s.closeAll()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		s.add(c)
	}
}
