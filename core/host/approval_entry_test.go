package host

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

type memoryListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newMemoryListener() *memoryListener {
	return &memoryListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *memoryListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *memoryListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *memoryListener) Addr() net.Addr { return memoryAddr{} }

func (l *memoryListener) dial() (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

type memoryAddr struct{}

func (memoryAddr) Network() string { return "memory" }

func (memoryAddr) String() string { return "memory" }

func TestTheSessionEntryPointRefusesAnUnansweredBridgeQuestionAtItsDeskBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, _ := newLongWaitDesk(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		listener := newMemoryListener()
		defer closeRead(listener)
		startSessionEntry(ctx, desk, control.Serve(ctx, listener))

		bridge, err := listener.dial()
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer closeRead(bridge)
		req := contracts.ApprovalRequest{ID: "req-entry", Tool: "Bash", Subject: "git push"}
		sendBridgeQuestion(t, bridge, req)

		answers := scanBridgeAnswers(bridge)
		select {
		case answer := <-answers:
			if answer.ID != req.ID || answer.Allowed() {
				t.Fatalf("an unanswered bridge question must be refused, got %+v", answer)
			}
		case <-time.After(bridgeDeskBound() + time.Second):
			t.Fatal("the session never answered the bridge within the desk bound")
		}
		if desk.sessions.remembers("s1", approval.Request{Tool: "Bash", Subject: "git push"}) {
			t.Fatal("an unanswered bridge question recorded a rule")
		}
	})
}

func startSessionEntry(ctx context.Context, desk *approvalDesk, acc *control.Acceptor) {
	gw := &fanRecorder{}
	go runSessionIdentified(ctx, desk.sessions, "s1", "", []contracts.GatewaySet{{Gateway: gw, Reader: gw}}, acc, "", nil, nil, sessionSink{}, nil, sessionIdentity{}, desk, nil, io.Discard)
}

func sendBridgeQuestion(t *testing.T, bridge net.Conn, req contracts.ApprovalRequest) {
	t.Helper()
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := control.WriteEvent(bridge, contracts.Event{T: eventwire.EventApprovalAsk, Value: string(payload)}); err != nil {
		t.Fatalf("write question: %v", err)
	}
}

func scanBridgeAnswers(bridge net.Conn) <-chan contracts.ApprovalAnswer {
	answers := make(chan contracts.ApprovalAnswer, 1)
	go func() {
		logCleanup(control.ScanEvents(bridge, func(event contracts.Event) error {
			var answer contracts.ApprovalAnswer
			if event.T == eventwire.EventApprovalAnswer && json.Unmarshal([]byte(event.Value), &answer) == nil {
				answers <- answer
			}
			return nil
		}))
	}()
	return answers
}
