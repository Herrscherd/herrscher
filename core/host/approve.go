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

const approvalEventKind = "approval"

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

type approvalScope string

const (
	scopeOnce    approvalScope = "allow"
	scopeSession approvalScope = "allow-session"
	scopeAlways  approvalScope = "allow-always"
	scopeDeny    approvalScope = "deny"
)

var approvalScopes = []approvalScope{scopeOnce, scopeSession, scopeAlways, scopeDeny}

type approvalAnswer struct {
	scope  approvalScope
	reason string
}

func (a approvalAnswer) decision() approval.Decision {
	if a.scope == scopeDeny {
		return approval.Deny
	}
	return approval.Allow
}

type approvalRules interface {
	AddApprovalRule(rendered string) error
}

type riskJudge func(ctx context.Context, req approval.Request) (approval.Risk, error)

// PendingApproval is one waiting request, as an operator sees it.
type PendingApproval struct {
	ID      string        `json:"id"`
	Session string        `json:"session"`
	Tool    string        `json:"tool"`
	Subject string        `json:"subject"`
	Age     time.Duration `json:"age"`
}

type approvalTable struct {
	mu    sync.Mutex
	m     map[string]*pendingApproval
	judge riskJudge
	rules approvalRules
	warn  io.Writer
}

func newApprovalTable() *approvalTable {
	return &approvalTable{m: make(map[string]*pendingApproval), judge: registryRiskJudge, warn: os.Stderr}
}

func (t *approvalTable) withRules(rules approvalRules) *approvalTable {
	t.rules = rules
	return t
}

func (t *approvalTable) warnf(format string, args ...any) {
	logWriteFailure(fmt.Fprintf(t.warn, format, args...))
}

func (t *approvalTable) rated(ctx context.Context, sessions *sessionTable, session string, req approval.Request, fallback approval.Decision) approval.Decision {
	if risk, ok := sessions.verdict(session, req); ok {
		return risk.Decision()
	}
	if t.judge == nil {
		t.warnf("approvals: smart mode has no model to rate %s, so it is decided as in ask mode\n", describeRequest(req))
		return fallback
	}
	risk, err := t.judge(ctx, req)
	if err != nil {
		t.warnf("approvals: smart mode could not rate %s (%v), so it is decided as in ask mode\n", describeRequest(req), err)
		return fallback
	}
	sessions.setVerdict(session, req, risk)
	return risk.Decision()
}

func askApproval(ctx context.Context, approvals *approvalTable, sessions *sessionTable, session string, req approval.Request, pol approval.Policy, mode approval.Mode, wait time.Duration) (approval.Decision, string) {
	raw, matched := pol.Decide(req)
	decided := approval.Apply(mode, raw, matched)
	if decided == approval.Deny {
		return approval.Deny, fmt.Sprintf("herrscher: %s was denied by policy", describeRequest(req))
	}
	if sessions.remembers(session, req) {
		return approval.Allow, ""
	}
	if approval.Judged(mode, req.Tool, decided, matched) {
		decided = approvals.rated(ctx, sessions, session, req, decided)
	}
	if decided == approval.Allow {
		return approval.Allow, ""
	}

	if wait <= 0 {
		wait = state.DefaultApprovalWait
	}

	pending := &pendingApproval{
		id:      newApprovalID(),
		session: session,
		req:     req,
		asked:   time.Now(),
		answer:  make(chan approvalVerdict, 1),
	}
	release, refusal := registerApproval(approvals, pending)
	if refusal != "" {
		return approval.Deny, refusal
	}
	defer release()

	warnWaitOutlivesTheHook(os.Stderr, wait)

	octx, cancel := context.WithTimeout(ctx, wait)
	askApprovalOn(octx, sessions, session, approvalPrompt(req), pending.id, req, time.Now().Add(wait))
	cancel()

	select {
	case decision := <-pending.answer:
		if decision.decision == approval.Allow {
			return approval.Allow, ""
		}
		reason := decision.reason
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

func registerApproval(approvals *approvalTable, pending *pendingApproval) (func(), string) {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	if len(approvals.m) >= maxPendingApprovals {
		return nil, fmt.Sprintf("herrscher: %s was denied: %d tool calls across the fleet are already waiting for a human, which is the limit; each one answered frees a slot", describeRequest(pending.req), maxPendingApprovals)
	}
	mine := 0
	for _, other := range approvals.m {
		if other.session == pending.session {
			mine++
		}
	}
	if mine >= maxPendingPerSession {
		return nil, fmt.Sprintf("herrscher: %s was denied: session %q already has %d tool calls waiting for a human, which is the limit; wait for those to be answered, and ask about fewer things at once", describeRequest(pending.req), pending.session, maxPendingPerSession)
	}
	approvals.m[pending.id] = pending
	return func() {
		approvals.mu.Lock()
		delete(approvals.m, pending.id)
		approvals.mu.Unlock()
	}, ""
}

func warnWaitOutlivesTheHook(out io.Writer, wait time.Duration) {
	if wait <= agent.HookWait/2 {
		return
	}
	logWriteFailure(fmt.Fprintf(out, "approvals: a %s wait can hold the hook for twice that (a slow menu post, then the timer), longer than the %s the CLI gives it: past %s the tool call runs and nobody is told\n", wait, agent.HookWait, agent.HookWait))
}

func settleApproval(approvals *approvalTable, sessions *sessionTable, approvalID string, answer approvalAnswer) bool {
	approvals.mu.Lock()
	pending := approvals.m[approvalID]
	approvals.mu.Unlock()
	if pending == nil {
		return false
	}
	select {
	case pending.answer <- approvalVerdict{decision: answer.decision(), reason: answer.reason}:
	default:
		return true
	}
	if answer.decision() == approval.Allow {
		approvals.remember(sessions, pending, answer.scope)
	}
	return true
}

func (t *approvalTable) remember(sessions *sessionTable, pending *pendingApproval, scope approvalScope) {
	switch scope {
	case scopeSession:
		if driver := sessions.lookup(pending.session); driver != nil {
			driver.remember(pending.req)
			return
		}
		t.warnf("approvals: %s was allowed once: session %q is not running, so it cannot remember the answer\n", describeRequest(pending.req), pending.session)
	case scopeAlways:
		t.addRule(pending.req)
	case scopeOnce, scopeDeny:
	}
}

func (t *approvalTable) addRule(req approval.Request) {
	rule, ok := approval.RuleFor(req.Tool, req.Subject)
	if !ok {
		t.warnf("approvals: %s was allowed once: it has no exact subject a rule could name without matching more\n", describeRequest(req))
		return
	}
	if t.rules == nil {
		t.warnf("approvals: %s was allowed once: no rule store is attached, so no rule was written\n", describeRequest(req))
		return
	}
	if err := t.rules.AddApprovalRule(rule.String()); err != nil {
		t.warnf("approvals: %s was allowed once: the rule %q was not saved: %v\n", describeRequest(req), rule.String(), err)
	}
}

func (s *sessionTable) verdict(session string, req approval.Request) (approval.Risk, bool) {
	if s == nil {
		return "", false
	}
	driver := s.lookup(session)
	if driver == nil {
		return "", false
	}
	return driver.verdict(req)
}

func (s *sessionTable) setVerdict(session string, req approval.Request, risk approval.Risk) {
	if s == nil {
		return
	}
	if driver := s.lookup(session); driver != nil {
		driver.setVerdict(req, risk)
	}
}

func (s *sessionTable) remembers(session string, req approval.Request) bool {
	if s == nil {
		return false
	}
	driver := s.lookup(session)
	return driver != nil && driver.remembered(req)
}

func pendingApprovals(approvals *approvalTable) []PendingApproval {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	out := make([]PendingApproval, 0, len(approvals.m))
	for _, pending := range approvals.m {
		out = append(out, PendingApproval{
			ID:      pending.id,
			Session: pending.session,
			Tool:    pending.req.Tool,
			Subject: clip(pending.req.Subject),
			Age:     time.Since(pending.asked).Truncate(time.Second),
		})
	}
	return out
}

func answerApprovalPick(approvals *approvalTable, sessions *sessionTable, session, value string) bool {
	if !strings.HasPrefix(value, approvalPickPrefix) {
		return false
	}
	approvalID, answer, ok := parseApprovalPick(value)
	if !ok {
		return true
	}
	if owner, known := approvalSession(approvals, approvalID); known && owner != session {
		return true
	}
	settleApproval(approvals, sessions, approvalID, answer)
	return true
}

func parseApprovalPick(value string) (string, approvalAnswer, bool) {
	rest := strings.TrimPrefix(value, approvalPickPrefix)
	approvalID, verdict, ok := strings.Cut(rest, ":")
	if !ok {
		return "", approvalAnswer{}, false
	}
	word, reason, _ := strings.Cut(verdict, ":")
	scope, known := scopeNamed(word)
	if !known {
		return "", approvalAnswer{}, false
	}
	return approvalID, approvalAnswer{scope: scope, reason: operatorRefusal(reason)}, true
}

func scopeNamed(word string) (approvalScope, bool) {
	for _, scope := range approvalScopes {
		if string(scope) == word {
			return scope, true
		}
	}
	return "", false
}

func operatorRefusal(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	return "herrscher: the operator refused: " + reason
}

func approvalSession(approvals *approvalTable, approvalID string) (string, bool) {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	pending := approvals.m[approvalID]
	if pending == nil {
		return "", false
	}
	return pending.session, true
}

func newApprovalID() string {
	var randomBytes [4]byte
	_, _ = rand.Read(randomBytes[:])
	return hex.EncodeToString(randomBytes[:])
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

func clip(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r", " "), "\n", " ")
	runes := []rune(text)
	if len(runes) > subjectMaxLen {
		return string(runes[:subjectMaxLen]) + "…"
	}
	return string(runes)
}

func approvalChoices(approvalID string, req approval.Request) []contracts.Choice {
	choices := make([]contracts.Choice, 0, len(approvalScopes))
	for _, scope := range approvalScopes {
		choices = append(choices, contracts.Choice{Label: scopeLabel(scope, req), Value: approvalPickPrefix + approvalID + ":" + string(scope)})
	}
	return choices
}

func scopeLabel(scope approvalScope, req approval.Request) string {
	if scope != scopeAlways {
		return string(scope)
	}
	rule, ok := approval.RuleFor(req.Tool, req.Subject)
	if !ok {
		return string(scope)
	}
	return string(scope) + " " + rule.Tool + "(" + rule.Pattern + ")"
}
