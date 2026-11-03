package host

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/approval"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const approvalPickPrefix = "herrscher-approval:"

const subjectMaxLen = 200

const (
	maxPendingPerSession = 8
	maxPendingApprovals  = 40
)

type pendingApproval struct {
	id      string
	session string
	req     approval.Request
	asked   time.Time
	answer  chan approvalVerdict
}

type approvalVerdict struct {
	decision approval.Decision
	reason   string
}

// PendingApproval is one waiting request, as an operator sees it.
type PendingApproval struct {
	ID      string        `json:"id"`
	Session string        `json:"session"`
	Tool    string        `json:"tool"`
	Subject string        `json:"subject"`
	Age     time.Duration `json:"age"`
}

type approvalTable struct {
	mu      sync.Mutex
	m       map[string]*pendingApproval
	changed changeNotifier
}

func newApprovalTable() *approvalTable {
	return &approvalTable{m: make(map[string]*pendingApproval)}
}

func askApproval(ctx context.Context, approvals *approvalTable, sessions *sessionTable, session string, req approval.Request, pol approval.Policy, mode approval.Mode, wait time.Duration) (approval.Decision, string) {
	raw, matched := pol.Decide(req)
	switch approval.Apply(mode, raw, matched) {
	case approval.Allow:
		return approval.Allow, ""
	case approval.Deny:
		return approval.Deny, fmt.Sprintf("herrscher: %s was denied by policy", describeRequest(req))
	case approval.Ask:
	}

	if wait <= 0 {
		wait = state.DefaultApprovalWait
	}

	p := &pendingApproval{
		id:      newApprovalID(),
		session: session,
		req:     req,
		asked:   time.Now(),
		answer:  make(chan approvalVerdict, 1),
	}
	release, refusal := registerApproval(approvals, p)
	if refusal != "" {
		return approval.Deny, refusal
	}
	defer release()

	warnWaitOutlivesTheHook(os.Stderr, wait)

	octx, cancel := context.WithTimeout(ctx, wait)
	askApprovalOn(octx, sessions, session, approvalPrompt(req), p.id)
	cancel()

	select {
	case v := <-p.answer:
		if v.decision == approval.Allow {
			return approval.Allow, ""
		}
		reason := v.reason
		if reason == "" {
			reason = fmt.Sprintf("herrscher: %s was denied by the operator", describeRequest(req))
		}
		return approval.Deny, reason
	case <-time.After(wait):
		return approval.Deny, fmt.Sprintf("herrscher: nobody answered within %s, so %s was denied", wait, describeRequest(req))
	case <-ctx.Done():
		return approval.Deny, fmt.Sprintf("herrscher: the wait for approval ended, so %s was denied", describeRequest(req))
	}
}

func registerApproval(approvals *approvalTable, p *pendingApproval) (func(), string) {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	if len(approvals.m) >= maxPendingApprovals {
		return nil, fmt.Sprintf("herrscher: %s was denied: %d tool calls across the fleet are already waiting for a human, which is the limit; each one answered frees a slot", describeRequest(p.req), maxPendingApprovals)
	}
	mine := 0
	for _, other := range approvals.m {
		if other.session == p.session {
			mine++
		}
	}
	if mine >= maxPendingPerSession {
		return nil, fmt.Sprintf("herrscher: %s was denied: session %q already has %d tool calls waiting for a human, which is the limit; wait for those to be answered, and ask about fewer things at once", describeRequest(p.req), p.session, maxPendingPerSession)
	}
	approvals.m[p.id] = p
	approvals.changed.signal()
	return func() {
		approvals.mu.Lock()
		delete(approvals.m, p.id)
		approvals.mu.Unlock()
		approvals.changed.signal()
	}, ""
}

func warnWaitOutlivesTheHook(w io.Writer, wait time.Duration) {
	if wait <= agent.HookWait/2 {
		return
	}
	logWriteFailure(fmt.Fprintf(w, "approvals: a %s wait can hold the hook for twice that (a slow menu post, then the timer), longer than the %s the CLI gives it: past %s the tool call runs and nobody is told\n", wait, agent.HookWait, agent.HookWait))
}

func answerApproval(approvals *approvalTable, id string, d approval.Decision, reason string) bool {
	approvals.mu.Lock()
	p := approvals.m[id]
	approvals.mu.Unlock()
	if p == nil {
		return false
	}
	select {
	case p.answer <- approvalVerdict{decision: d, reason: reason}:
	default:
	}
	return true
}

func pendingApprovals(approvals *approvalTable) []PendingApproval {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	out := make([]PendingApproval, 0, len(approvals.m))
	for _, p := range approvals.m {
		out = append(out, PendingApproval{
			ID:      p.id,
			Session: p.session,
			Tool:    p.req.Tool,
			Subject: clip(p.req.Subject),
			Age:     time.Since(p.asked).Truncate(time.Second),
		})
	}
	return out
}

func answerApprovalPick(approvals *approvalTable, session, value string) bool {
	rest, ok := strings.CutPrefix(value, approvalPickPrefix)
	if !ok {
		return false
	}
	id, verdict, ok := strings.Cut(rest, ":")
	if !ok {
		return true
	}
	if owner, known := approvalSession(approvals, id); known && owner != session {
		return true
	}
	d := approval.Deny
	if verdict == string(approval.Allow) {
		d = approval.Allow
	}
	answerApproval(approvals, id, d, "")
	return true
}

func approvalSession(approvals *approvalTable, id string) (string, bool) {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	p := approvals.m[id]
	if p == nil {
		return "", false
	}
	return p.session, true
}

func newApprovalID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func describeRequest(req approval.Request) string {
	if req.Subject == "" {
		return req.Tool
	}
	return req.Tool + "(" + clip(req.Subject) + ")"
}

func approvalPrompt(req approval.Request) string {
	return "allow " + describeRequest(req) + "?"
}

func clip(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	r := []rune(s)
	if len(r) > subjectMaxLen {
		return string(r[:subjectMaxLen]) + "…"
	}
	return string(r)
}

func approvalChoices(id string) []contracts.Choice {
	return []contracts.Choice{
		{Label: "allow", Value: approvalPickPrefix + id + ":allow"},
		{Label: "deny", Value: approvalPickPrefix + id + ":deny"},
	}
}
