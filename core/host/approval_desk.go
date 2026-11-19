package host

import (
	"context"
	"io"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type approvalDesk struct {
	messages  *hostMessages
	lang      languageSource
	warn      io.Writer
	store     *state.State
	agents    *agent.Store
	approvals *approvalTable
	sessions  *sessionTable
}

func newApprovalDesk(messages *hostMessages, lang languageSource, warn io.Writer, store *state.State, agents *agent.Store, approvals *approvalTable, sessions *sessionTable) *approvalDesk {
	return &approvalDesk{messages: messages, lang: lang, warn: warn, store: store, agents: agents, approvals: approvals, sessions: sessions}
}

func (d *approvalDesk) decide(ctx context.Context, session string, req approval.Request) (approval.Decision, string) {
	pol, mode := sessionPolicy(d.messages, d.lang, d.warn, d.store, d.agents, session)
	return askApproval(ctx, d.approvals, d.sessions, session, req, pol, mode, d.store.ApprovalWait())
}

func (d *approvalDesk) asker(session string) contracts.ApprovalAsker {
	return func(ctx context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		decision, reason := d.decide(ctx, session, approval.Request{Tool: strings.TrimSpace(req.Tool), Subject: req.Subject})
		answer := contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionDeny, Reason: reason}
		if decision == approval.Allow {
			answer.Decision = contracts.DecisionAllow
		}
		return answer
	}
}
