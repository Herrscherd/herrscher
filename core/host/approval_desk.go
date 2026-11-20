package host

import (
	"context"
	"io"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const approvalDeskMargin = 30 * time.Second

func bridgeDeskBound() time.Duration {
	return BridgeApprovalWait() - approvalDeskMargin
}

func hookDeskBound() time.Duration {
	return hookAnswerWait() - approvalDeskMargin
}

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

func (d *approvalDesk) decideWithin(ctx context.Context, bound time.Duration, session string, req approval.Request) (approval.Decision, string) {
	bounded, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	pol, mode := sessionPolicy(d.messages, d.lang, d.warn, d.store, d.agents, session)
	return askApproval(bounded, d.approvals, d.sessions, session, req, pol, mode, d.store.ApprovalWait(), bound)
}

func (d *approvalDesk) asker(session string, bound time.Duration) contracts.ApprovalAsker {
	return func(ctx context.Context, req contracts.ApprovalRequest) contracts.ApprovalAnswer {
		decision, reason := d.decideWithin(ctx, bound, session, approval.Request{Tool: strings.TrimSpace(req.Tool), Subject: req.Subject})
		answer := contracts.ApprovalAnswer{ID: req.ID, Decision: contracts.DecisionDeny, Reason: reason}
		if decision == approval.Allow {
			answer.Decision = contracts.DecisionAllow
		}
		return answer
	}
}
