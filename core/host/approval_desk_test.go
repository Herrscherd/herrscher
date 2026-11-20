package host

import (
	"context"
	"errors"
	"io"
	"strings"
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

func stubSeedDesk() *approvalDesk {
	return newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, nil, nil, newApprovalTable(io.Discard), newSessionTable(newHostMessages(), clitext.NewMessages()))
}

func hubDesk(st *state.State, approvals *approvalTable, sessions *sessionTable) *approvalDesk {
	return newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvals, sessions)
}

func seedDesk(t *testing.T) *approvalDesk {
	t.Helper()
	desk, _, _ := newTestDesk(t)
	return desk
}

func TestApprovalDeskAskerReturnsTheOperatorsAllow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab, reg := newTestDesk(t)
		answers := make(chan contracts.ApprovalAnswer, 1)
		go func() {
			answers <- desk.asker("s1")(context.Background(), contracts.ApprovalRequest{ID: "req-1", Tool: "Bash", Subject: "git push"})
		}()
		id := awaitPending(t, approvalTab, 1)[0].ID
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
		id := awaitPending(t, approvalTab, 1)[0].ID
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
		awaitPending(t, approvalTab, 1)
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

func TestSeedWithoutADeskFailsBeforeItsBackendIsBuilt(t *testing.T) {
	built := false
	runtime := oneShotSeedRuntime{
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		backend: func(context.Context, state.Session) (contracts.Backend, error) {
			built = true
			return nil, errors.New("stop")
		},
	}
	_, err := runOneShotSeedWithIDRuntime(context.Background(), state.Session{Name: "s1", ChannelID: "channel"}, "tâche", newTurnID(), nil, runtime)
	if !errors.Is(err, errSeedWithoutApprovalDesk) {
		t.Fatalf("error %v, want the seed without an approval desk", err)
	}
	if !strings.Contains(err.Error(), "approval desk") {
		t.Fatalf("error %q is not the localized refusal", err.Error())
	}
	if built {
		t.Fatal("a seed without an approval desk built its backend anyway")
	}
}

func TestLocalSeedWithoutADeskStillRunsItsBackend(t *testing.T) {
	built := false
	failure := errors.New("stop after the backend was asked for")
	runtime := oneShotSeedRuntime{
		localRun:    true,
		persists:    newPersistWrites(),
		messages:    newHostMessages(),
		cliMessages: clitext.NewMessages(),
		backend: func(context.Context, state.Session) (contracts.Backend, error) {
			built = true
			return nil, failure
		},
	}
	_, err := runOneShotSeedWithIDRuntime(context.Background(), state.Session{Name: "s1", ChannelID: "channel"}, "tâche", newTurnID(), nil, runtime)
	if !built {
		t.Fatalf("the local seed never built its backend: %v", err)
	}
}

func TestApprovalDeskAskerKeepsTwoConcurrentRequestsApart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab, reg := newTestDesk(t)
		answers := make(chan contracts.ApprovalAnswer, 2)
		for _, id := range []string{"req-5", "req-6"} {
			go func() {
				answers <- desk.asker("s1")(context.Background(), contracts.ApprovalRequest{ID: id, Tool: "Bash", Subject: "git push " + id})
			}()
		}
		for _, pending := range awaitPending(t, approvalTab, 2) {
			if _, err := cli.Plain(reg.Dispatch(context.Background(), []string{"approve", "allow", pending.ID})); err != nil {
				t.Fatalf("allow %s: %v", pending.ID, err)
			}
		}
		seen := make(map[string]bool)
		for range 2 {
			answer := <-answers
			if !answer.Allowed() {
				t.Fatalf("answer %+v, want allowed", answer)
			}
			seen[answer.ID] = true
		}
		if !seen["req-5"] || !seen["req-6"] {
			t.Fatalf("answers went to %v, want each request answered once", seen)
		}
	})
}
