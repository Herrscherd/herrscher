package host

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func newTestDesk(t *testing.T) (*approvalDesk, *approvalTable, *cli.Registry) {
	t.Helper()
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages())
	reg, st := approveReg(t, approvalTab, sessionTab)
	if err := st.AddApprovalRule("ask Bash(git push*)"); err != nil {
		t.Fatalf("AddApprovalRule: %v", err)
	}
	desk := newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab)
	return desk, approvalTab, reg
}

func TestApprovalDeskAskerReturnsTheOperatorsAllow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab, reg := newTestDesk(t)
		answers := make(chan contracts.ApprovalAnswer, 1)
		go func() {
			answers <- desk.asker("s1")(context.Background(), contracts.ApprovalRequest{ID: "req-1", Tool: "Bash", Subject: "git push"})
		}()
		id := awaitPending(t, approvalTab)[0].ID
		if _, err := cli.Plain(reg.Dispatch(context.Background(), []string{"approve", "allow", id})); err != nil {
			t.Fatalf("allow: %v", err)
		}
		answer := <-answers
		if answer.ID != "req-1" || !answer.Allowed() {
			t.Fatalf("answer %+v, want req-1 allowed", answer)
		}
	})
}

func TestApprovalDeskAskerReturnsTheOperatorsDeny(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab, reg := newTestDesk(t)
		answers := make(chan contracts.ApprovalAnswer, 1)
		go func() {
			answers <- desk.asker("s1")(context.Background(), contracts.ApprovalRequest{ID: "req-2", Tool: "Bash", Subject: "git push"})
		}()
		id := awaitPending(t, approvalTab)[0].ID
		if _, err := cli.Plain(reg.Dispatch(context.Background(), []string{"approve", "deny", id})); err != nil {
			t.Fatalf("deny: %v", err)
		}
		answer := <-answers
		if answer.ID != "req-2" || answer.Allowed() || answer.Decision != contracts.DecisionDeny {
			t.Fatalf("answer %+v, want req-2 denied", answer)
		}
	})
}

func TestApprovalDeskAskerDeniesWhenNobodyAnswers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, _, _ := newTestDesk(t)
		answer := desk.asker("s1")(context.Background(), contracts.ApprovalRequest{ID: "req-3", Tool: "Bash", Subject: "git push"})
		if answer.ID != "req-3" || answer.Allowed() {
			t.Fatalf("answer %+v, want req-3 refused after the wait", answer)
		}
	})
}

func TestApprovalDeskAskerDeniesWhenTheTurnIsCancelled(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab, _ := newTestDesk(t)
		ctx, cancel := context.WithCancel(context.Background())
		answers := make(chan contracts.ApprovalAnswer, 1)
		go func() {
			answers <- desk.asker("s1")(ctx, contracts.ApprovalRequest{ID: "req-4", Tool: "Bash", Subject: "git push"})
		}()
		awaitPending(t, approvalTab)
		cancel()
		if answer := <-answers; answer.Allowed() {
			t.Fatalf("answer %+v, want refused once the turn is cancelled", answer)
		}
	})
}

func TestSeedBridgeBuildsItsBackendWithTheSessionAsker(t *testing.T) {
	desk, _, _ := newTestDesk(t)
	seen := make(chan bool, 1)
	failure := errors.New("stop after the context was inspected")
	runtime := oneShotSeedRuntime{
		desk: desk,
		backend: func(ctx context.Context, _ state.Session) (contracts.Backend, error) {
			_, ok := contracts.ApprovalAskerFrom(ctx)
			seen <- ok
			return nil, failure
		},
	}
	bridgeErr := make(chan error, 1)
	runOneShotBridge(context.Background(), func() {}, state.Session{Name: "s1"}, nil, runtime, make(chan contracts.Event), make(chan contracts.Event, 1), bridgeErr)
	if !<-seen {
		t.Fatal("the backend was built without an approval asker in its context")
	}
	if err := <-bridgeErr; !errors.Is(err, failure) {
		t.Fatalf("bridge error %v, want the backend failure", err)
	}
}

func TestSeedBridgeWithoutADeskLeavesTheContextBare(t *testing.T) {
	seen := make(chan bool, 1)
	runtime := oneShotSeedRuntime{
		backend: func(ctx context.Context, _ state.Session) (contracts.Backend, error) {
			_, ok := contracts.ApprovalAskerFrom(ctx)
			seen <- ok
			return nil, errors.New("stop")
		},
	}
	runOneShotBridge(context.Background(), func() {}, state.Session{Name: "s1"}, nil, runtime, make(chan contracts.Event), make(chan contracts.Event, 1), make(chan error, 1))
	if <-seen {
		t.Fatal("a runtime without a desk handed its backend an approval asker")
	}
}
