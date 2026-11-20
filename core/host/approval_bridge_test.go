package host

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
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

func startDaemonSide(t *testing.T, daemonSide *control.Conn, approvals bridgeApprovals) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go serveConn(ctx, daemonSide, nil, make(chan contracts.Event), make(chan struct{}, 1), approvals)
}

func deskApprovals(asker contracts.ApprovalAsker) bridgeApprovals {
	return bridgeApprovals{asker: asker, messages: newHostMessages(), lang: fixedLanguageSource(i18n.English)}
}

func readBridgeAnswers(t *testing.T, bridgeSide *control.Conn) <-chan contracts.ApprovalAnswer {
	t.Helper()
	answers := make(chan contracts.ApprovalAnswer, 4)
	go func() {
		logCleanup(bridgeSide.Scan(func(event contracts.Event) error {
			var answer contracts.ApprovalAnswer
			if event.T == eventwire.EventApprovalAnswer && json.Unmarshal([]byte(event.Value), &answer) == nil {
				answers <- answer
			}
			return nil
		}))
	}()
	return answers
}

func awaitBridgeAnswer(t *testing.T, answers <-chan contracts.ApprovalAnswer) contracts.ApprovalAnswer {
	t.Helper()
	select {
	case answer := <-answers:
		return answer
	case <-time.After(5 * time.Second):
		t.Fatal("the daemon never answered the bridge")
	}
	return contracts.ApprovalAnswer{}
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
	startDaemonSide(t, daemonSide, deskApprovals(desk.asker("s1")))
	answers := readBridgeAnswers(t, bridgeSide)

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

	answer := awaitBridgeAnswer(t, answers)
	if answer.ID != "req-9" || !answer.Allowed() {
		t.Fatalf("answer %+v, want req-9 allowed", answer)
	}
}

func TestDaemonRefusesARequestItCannotDecodeWithItsOwnID(t *testing.T) {
	desk, approvalTab, _ := newTestDesk(t)
	daemonSide, bridgeSide := connectedPair(t)
	startDaemonSide(t, daemonSide, deskApprovals(desk.asker("s1")))
	answers := readBridgeAnswers(t, bridgeSide)
	if err := bridgeSide.Write(contracts.Event{T: eventwire.EventApprovalAsk, Value: `{"ID":"req-10","Tool":5}`}); err != nil {
		t.Fatalf("write unreadable request: %v", err)
	}
	answer := awaitBridgeAnswer(t, answers)
	if answer.ID != "req-10" || answer.Allowed() || answer.Reason == "" {
		t.Fatalf("answer %+v, want req-10 refused with a reason", answer)
	}
	if pending := pendingApprovals(approvalTab); len(pending) != 0 {
		t.Fatalf("pending %+v, want nothing asked of the operator", pending)
	}
}

func TestDaemonRefusesWithAReasonWhenTheSessionHasNoDesk(t *testing.T) {
	daemonSide, bridgeSide := connectedPair(t)
	startDaemonSide(t, daemonSide, deskApprovals(nil))
	answers := readBridgeAnswers(t, bridgeSide)
	if err := bridgeSide.Write(contracts.Event{T: eventwire.EventApprovalAsk, Value: `{"ID":"req-11","Tool":"Bash","Subject":"git push"}`}); err != nil {
		t.Fatalf("write request: %v", err)
	}
	answer := awaitBridgeAnswer(t, answers)
	if answer.ID != "req-11" || answer.Allowed() || !strings.Contains(answer.Reason, "approval desk") {
		t.Fatalf("answer %+v, want req-11 refused with the reason that the session has no approval desk", answer)
	}
}
