package host

import (
	"io"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

func TestASettleAfterTheDeadlineIsRefusedWithoutARule(t *testing.T) {
	approvalTab := newApprovalTable(io.Discard)
	sessionTab := newSessionTable(newHostMessages(), clitext.NewMessages(), nil, nil)
	sessionTab.register("s1", &sessionDriver{messages: newHostMessages(), cliMessages: clitext.NewMessages(), persists: newPersistWrites(), name: "s1"})
	p := newTestPendingApproval("s1")
	p.req = approval.Request{Tool: "Bash", Subject: "git push"}
	p.deadline = time.Now().Add(-time.Second)
	unregister := registerTestApproval(t, approvalTab, p)
	defer unregister()

	if settleApproval(approvalTab, p.id, approvalAnswer{scope: scopeSession}) {
		t.Fatal("a session answer after the deadline was taken")
	}
	assertSettlement(t, p, false)
	if sessionTab.remembers("s1", p.req) {
		t.Fatal("a session answer after the deadline recorded a rule")
	}
}
