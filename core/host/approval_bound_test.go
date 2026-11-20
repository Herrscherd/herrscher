package host

import (
	"context"
	"io"
	"testing"
	"testing/synctest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

func newLongWaitDesk(t *testing.T) (*approvalDesk, *approvalTable) {
	t.Helper()
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages())
	_, st := approveReg(t, approvalTab, sessionTab)
	if err := st.AddApprovalRule("ask Bash(git push*)"); err != nil {
		t.Fatalf("AddApprovalRule: %v", err)
	}
	st.ApprovalTimeout = "20m"
	sessionTab.register("s1", &sessionDriver{messages: newHostMessages(), cliMessages: clitext.NewMessages(), persists: newPersistWrites(), name: "s1"})
	desk := newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab)
	return desk, approvalTab
}

func askBridgeInBackground(desk *approvalDesk) <-chan contracts.ApprovalAnswer {
	answers := make(chan contracts.ApprovalAnswer, 1)
	go func() {
		answers <- desk.askerFor(bridgeConsumer, "s1")(context.Background(), contracts.ApprovalRequest{ID: "req-bridge", Tool: "Bash", Subject: "git push"})
	}()
	return answers
}

func TestTheBridgeQuestionLeavesNothingBehindAtItsBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab := newLongWaitDesk(t)
		answers := askBridgeInBackground(desk)
		id := awaitPending(t, approvalTab, 1)[0].ID

		if answer := <-answers; answer.Allowed() {
			t.Fatalf("an unanswered question must be refused, got %+v", answer)
		}
		assertNoLateRule(t, desk, approvalTab, id, approval.Request{Tool: "Bash", Subject: "git push"})
	})
}

func TestTheTokenQuestionLeavesNothingBehindAtItsBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab := newLongWaitDesk(t)
		req := approval.Request{Tool: "Bash", Subject: "git push"}
		decisions := make(chan approval.Decision, 1)
		go func() {
			decision, _ := desk.decideFor(context.Background(), tokenConsumer, "s1", req)
			decisions <- decision
		}()
		id := awaitPending(t, approvalTab, 1)[0].ID

		if decision := <-decisions; decision != approval.Deny {
			t.Fatalf("an unanswered question must be refused, got %q", decision)
		}
		assertNoLateRule(t, desk, approvalTab, id, req)
	})
}

func TestASessionVerdictArrivingAtTheBoundGrantsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab := newLongWaitDesk(t)
		answers := askBridgeInBackground(desk)
		id := awaitPending(t, approvalTab, 1)[0].ID

		time.Sleep(bridgeDeskBound())
		if settleApproval(approvalTab, id, approvalAnswer{scope: scopeSession}) {
			t.Fatal("a session answer arriving at the bound was reported as taken")
		}
		if answer := <-answers; answer.Allowed() {
			t.Fatalf("a session answer at the bound allowed the call: %+v", answer)
		}
		if desk.sessions.remembers("s1", approval.Request{Tool: "Bash", Subject: "git push"}) {
			t.Fatal("a session answer at the bound recorded a rule for a call already refused")
		}
	})
}

func TestASessionVerdictBeforeTheBoundIsGrantedAndRemembered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab := newLongWaitDesk(t)
		answers := askBridgeInBackground(desk)
		id := awaitPending(t, approvalTab, 1)[0].ID

		time.Sleep(bridgeDeskBound() - time.Minute)
		if !settleApproval(approvalTab, id, approvalAnswer{scope: scopeSession}) {
			t.Fatal("a session answer given inside the bound was not taken")
		}
		if answer := <-answers; !answer.Allowed() {
			t.Fatalf("a session answer inside the bound must allow the call, got %+v", answer)
		}
		if !desk.sessions.remembers("s1", approval.Request{Tool: "Bash", Subject: "git push"}) {
			t.Fatal("a session answer inside the bound was not remembered")
		}
	})
}

func assertNoLateRule(t *testing.T, desk *approvalDesk, approvalTab *approvalTable, id string, req approval.Request) {
	t.Helper()
	if pending := pendingApprovals(approvalTab); len(pending) != 0 {
		t.Fatalf("%d approvals still pending after the bound", len(pending))
	}
	if settleApproval(approvalTab, id, approvalAnswer{scope: scopeSession}) {
		t.Fatal("a late answer reached a question the bound had already ended")
	}
	if desk.sessions.remembers("s1", req) {
		t.Fatal("a late session answer recorded a rule for a call already refused")
	}
}
