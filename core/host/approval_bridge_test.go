package host

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

func connectedPair(t *testing.T) (*control.Conn, *control.Conn) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	path := privateSocketPath(t)
	listener, err := control.Listen(ctx, path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	acceptor := control.Serve(ctx, listener)
	t.Cleanup(func() { closeRead(acceptor) })
	bridgeSide, err := control.DialContext(ctx, path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { closeRead(bridgeSide) })
	daemonSide, ok := <-acceptor.Conns()
	if !ok {
		t.Fatal("the daemon never accepted the bridge")
	}
	t.Cleanup(func() { closeRead(daemonSide) })
	return daemonSide, bridgeSide
}

func startDaemonSide(t *testing.T, daemonSide *control.Conn, asker contracts.ApprovalAsker) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go serveConn(ctx, daemonSide, nil, make(chan contracts.Event), make(chan struct{}, 1), asker)
}

func awaitPendingLive(t *testing.T, tab *approvalTable, want int) []PendingApproval {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pending := pendingApprovals(tab); len(pending) == want {
			return pending
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("want %d pending approvals, have %+v", want, pendingApprovals(tab))
	return nil
}

func TestDaemonAnswersTheBridgeWithTheOperatorsDecisionForItsSession(t *testing.T) {
	desk, approvalTab, reg := newTestDesk(t)
	daemonSide, bridgeSide := connectedPair(t)
	startDaemonSide(t, daemonSide, desk.asker("s1"))

	answers := make(chan contracts.Event, 1)
	go func() {
		logCleanup(bridgeSide.Scan(func(event contracts.Event) error {
			answers <- event
			return nil
		}))
	}()

	request := `{"ID":"req-9","Tool":"Bash","Subject":"git push","Session":"victim"}`
	if err := bridgeSide.Write(contracts.Event{T: eventwire.EventApprovalAsk, Value: request}); err != nil {
		t.Fatalf("write request: %v", err)
	}
	pending := awaitPendingLive(t, approvalTab, 1)
	if pending[0].Session != "s1" {
		t.Fatalf("the request was asked for session %q, want s1: the payload must not choose the session", pending[0].Session)
	}
	if _, err := cli.Plain(reg.Dispatch(context.Background(), []string{"approve", "allow", pending[0].ID})); err != nil {
		t.Fatalf("allow: %v", err)
	}

	event := <-answers
	if event.T != eventwire.EventApprovalAnswer {
		t.Fatalf("event %q, want the approval answer", event.T)
	}
	var answer contracts.ApprovalAnswer
	if err := json.Unmarshal([]byte(event.Value), &answer); err != nil {
		t.Fatalf("answer %q unreadable: %v", event.Value, err)
	}
	if answer.ID != "req-9" || !answer.Allowed() {
		t.Fatalf("answer %+v, want req-9 allowed", answer)
	}
}

func TestDaemonIgnoresARequestItCannotDecode(t *testing.T) {
	desk, approvalTab, _ := newTestDesk(t)
	daemonSide, bridgeSide := connectedPair(t)
	startDaemonSide(t, daemonSide, desk.asker("s1"))
	if err := bridgeSide.Write(contracts.Event{T: eventwire.EventApprovalAsk, Value: "not json"}); err != nil {
		t.Fatalf("write unreadable request: %v", err)
	}
	valid := `{"ID":"req-10","Tool":"Bash","Subject":"git push"}`
	if err := bridgeSide.Write(contracts.Event{T: eventwire.EventApprovalAsk, Value: valid}); err != nil {
		t.Fatalf("write valid request: %v", err)
	}
	pending := awaitPendingLive(t, approvalTab, 1)
	if pending[0].ID == "" || pending[0].Subject != "git push" {
		t.Fatalf("pending %+v, want only the valid request", pending)
	}
}
