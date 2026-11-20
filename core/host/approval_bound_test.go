package host

import (
	"context"
	"io"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

const boundedQuestionWait = 200 * time.Millisecond

func newLongWaitDesk(t *testing.T) (*approvalDesk, *approvalTable) {
	t.Helper()
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages())
	_, st := approveReg(t, approvalTab, sessionTab)
	if err := st.AddApprovalRule("ask Bash(git push*)"); err != nil {
		t.Fatalf("AddApprovalRule: %v", err)
	}
	st.ApprovalTimeout = "20m"
	desk := newApprovalDesk(newHostMessages(), fixedLanguage(i18n.English), io.Discard, st, nil, approvalTab, sessionTab)
	return desk, approvalTab
}

func TestTheBridgeQuestionLeavesNothingBehindAtItsBound(t *testing.T) {
	desk, approvalTab := newLongWaitDesk(t)
	req := approval.Request{Tool: "Bash", Subject: "git push"}
	answers := make(chan contracts.ApprovalAnswer, 1)
	go func() {
		answers <- desk.asker("s1", boundedQuestionWait)(context.Background(), contracts.ApprovalRequest{ID: "req-bridge", Tool: "Bash", Subject: "git push"})
	}()
	id := awaitPendingLive(t, approvalTab, 1)[0].ID

	select {
	case answer := <-answers:
		if answer.Allowed() {
			t.Fatalf("an unanswered question must be refused, got %+v", answer)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the bridge question outlived its bound")
	}
	assertNoLateRule(t, desk, approvalTab, id, req)
}

func TestTheTokenQuestionLeavesNothingBehindAtItsBound(t *testing.T) {
	desk, approvalTab := newLongWaitDesk(t)
	req := approval.Request{Tool: "Bash", Subject: "git push"}
	decisions := make(chan approval.Decision, 1)
	go func() {
		decision, _ := desk.decideWithin(context.Background(), boundedQuestionWait, "s1", req)
		decisions <- decision
	}()
	id := awaitPendingLive(t, approvalTab, 1)[0].ID

	select {
	case decision := <-decisions:
		if decision != approval.Deny {
			t.Fatalf("an unanswered question must be refused, got %q", decision)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the token question outlived its bound")
	}
	assertNoLateRule(t, desk, approvalTab, id, req)
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
