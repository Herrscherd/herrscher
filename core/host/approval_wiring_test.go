package host

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

func askWiredInBackground(asker contracts.ApprovalAsker) <-chan contracts.ApprovalAnswer {
	answers := make(chan contracts.ApprovalAnswer, 1)
	go func() {
		answers <- asker(context.Background(), contracts.ApprovalRequest{ID: "req-wired", Tool: "Bash", Subject: "git push"})
	}()
	return answers
}

func TestTheBridgeWiringRefusesAnAnswerAfterItsDeskBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab := newLongWaitDesk(t)
		answers := askWiredInBackground(bridgeAskerFor(desk, "s1"))
		id := awaitPending(t, approvalTab, 1)[0].ID

		time.Sleep(bridgeDeskBound() + time.Second)
		if settleApproval(approvalTab, id, approvalAnswer{scope: scopeSession}) {
			t.Fatal("the bridge wiring took a session answer one second after its desk bound")
		}
		if answer := <-answers; answer.Allowed() {
			t.Fatalf("the bridge wiring allowed a call after its desk bound: %+v", answer)
		}
		assertNoLateRule(t, desk, approvalTab, id, approval.Request{Tool: "Bash", Subject: "git push"})
	})
}

func TestTheSeedWiringRefusesAnAnswerAfterItsDeskBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab := newLongWaitDesk(t)
		answers := askWiredInBackground(seedAskerFor(desk, "s1"))
		id := awaitPending(t, approvalTab, 1)[0].ID

		time.Sleep(hookDeskBound() + time.Second)
		if settleApproval(approvalTab, id, approvalAnswer{scope: scopeSession}) {
			t.Fatal("the seed wiring took a session answer one second after its desk bound")
		}
		if answer := <-answers; answer.Allowed() {
			t.Fatalf("the seed wiring allowed a call after its desk bound: %+v", answer)
		}
		assertNoLateRule(t, desk, approvalTab, id, approval.Request{Tool: "Bash", Subject: "git push"})
	})
}

func TestTheTokenWiringRefusesAnAnswerAfterItsDeskBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		desk, approvalTab := newLongWaitDesk(t)
		req := approval.Request{Tool: "Bash", Subject: "git push"}
		decisions := make(chan approval.Decision, 1)
		go func() {
			decision, _ := tokenDecision(context.Background(), desk, "s1", req)
			decisions <- decision
		}()
		id := awaitPending(t, approvalTab, 1)[0].ID

		time.Sleep(hookDeskBound() + time.Second)
		if settleApproval(approvalTab, id, approvalAnswer{scope: scopeSession}) {
			t.Fatal("the token wiring took a session answer one second after its desk bound")
		}
		if decision := <-decisions; decision != approval.Deny {
			t.Fatalf("the token wiring let a call through after its desk bound: %q", decision)
		}
		assertNoLateRule(t, desk, approvalTab, id, req)
	})
}
