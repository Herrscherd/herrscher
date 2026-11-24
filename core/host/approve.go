package host

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
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
	id       string
	session  string
	req      approval.Request
	asked    time.Time
	deadline time.Time
	answer   chan approvalVerdict
}

type approvalVerdict struct {
	decision approval.Decision
	reason   string
	scope    approvalScope
}

type approvalScope uint8

const (
	scopeOnce approvalScope = iota + 1
	scopeSession
	scopeAlways
	scopeDeny
)

func approvalScopes() []approvalScope {
	return []approvalScope{scopeOnce, scopeSession, scopeAlways, scopeDeny}
}

func (s approvalScope) String() string {
	switch s {
	case scopeOnce:
		return "allow"
	case scopeSession:
		return "allow-session"
	case scopeAlways:
		return "allow-always"
	case scopeDeny:
		return "deny"
	}
	return ""
}

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
	mu       sync.Mutex
	m        map[string]*pendingApproval
	judge    riskJudge
	rules    approvalRules
	warn     io.Writer
	messages *hostMessages
	lang     languageSource
}

func newApprovalTable(warn io.Writer) *approvalTable {
	return &approvalTable{
		m:        make(map[string]*pendingApproval),
		judge:    registryRiskJudge,
		warn:     warningWriter(warn),
		messages: newHostMessages(),
		lang:     fixedLanguageSource(i18n.English),
	}
}

func (t *approvalTable) withRules(rules approvalRules) *approvalTable {
	t.rules = rules
	return t
}

func (t *approvalTable) withMessages(messages *hostMessages, lang languageSource) *approvalTable {
	t.messages = messages
	t.lang = lang
	return t
}

func (t *approvalTable) text(key string, params map[string]any) string {
	return t.messages.text(t.lang(), key, params)
}

func (t *approvalTable) warnf(format string, args ...any) {
	logWriteFailure(fmt.Fprintf(t.warn, format, args...))
}

func (t *approvalTable) rated(ctx context.Context, sessions *sessionTable, session string, req approval.Request, fallback approval.Decision) approval.Decision {
	if risk, ok := sessions.verdict(session, req); ok {
		return risk.Decision()
	}
	if t.judge == nil {
		warnTo(t.messages, t.warn, t.lang(), "approval.smart_no_model", requestParams(req))
		return fallback
	}
	risk, err := t.judge(ctx, req)
	if errors.Is(err, errNoRiskModel) {
		warnTo(t.messages, t.warn, t.lang(), "approval.smart_no_model", requestParams(req))
		return fallback
	}
	if err != nil {
		params := requestParams(req)
		params["Detail"] = err.Error()
		warnTo(t.messages, t.warn, t.lang(), "approval.smart_rate_failed", params)
		return fallback
	}
	sessions.setVerdict(session, req, risk)
	return risk.Decision()
}

func askApproval(ctx context.Context, approvals *approvalTable, sessions *sessionTable, session string, req approval.Request, pol approval.Policy, mode approval.Mode, wait, bound time.Duration) (approval.Decision, string) {
	raw, matched := pol.Decide(req)
	decided := approval.Apply(mode, req.Tool, raw, matched)
	if decided == approval.Deny {
		return approval.Deny, approvals.denialReason(mode, req)
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
	if bound > 0 && wait > bound {
		approvals.warnWaitTruncated(wait, bound)
		wait = bound
	}

	now := time.Now()
	pending := &pendingApproval{
		id:       newApprovalID(),
		session:  session,
		req:      req,
		asked:    now,
		deadline: now.Add(wait),
		answer:   make(chan approvalVerdict, 1),
	}
	release, refusal := registerApproval(approvals, pending)
	if refusal != "" {
		return approval.Deny, refusal
	}
	defer release()

	octx, cancel := context.WithTimeout(ctx, wait)
	askApprovalOn(octx, sessions, session, pending.id, req, pending.deadline)
	cancel()

	verdict, refused := approvals.awaitVerdict(ctx, pending, wait)
	if buffered, ok := closeApproval(approvals, pending); ok {
		verdict, refused = buffered, ""
	}
	if refused != "" {
		return approval.Deny, refused
	}
	if verdict.decision != approval.Allow {
		reason := verdict.reason
		if reason == "" {
			reason = approvals.text("approval.denied", requestParams(req))
		}
		return approval.Deny, reason
	}
	approvals.remember(sessions, pending, verdict.scope)
	return approval.Allow, ""
}

func (t *approvalTable) awaitVerdict(ctx context.Context, pending *pendingApproval, wait time.Duration) (approvalVerdict, string) {
	select {
	case verdict := <-pending.answer:
		return verdict, ""
	case <-time.After(wait):
		params := requestParams(pending.req)
		params["Wait"] = humanWait(wait)
		return approvalVerdict{}, t.text("approval.timeout", params)
	case <-ctx.Done():
		return approvalVerdict{}, t.text("approval.ended", requestParams(pending.req))
	}
}

func humanWait(wait time.Duration) string {
	if wait < time.Second {
		return strconv.FormatInt(wait.Milliseconds(), 10) + "ms"
	}
	rest := wait.Round(time.Second)
	if rest < time.Minute {
		return strconv.FormatInt(int64(rest/time.Second), 10) + "s"
	}
	var out strings.Builder
	for _, unit := range []struct {
		size   time.Duration
		symbol string
	}{{time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}} {
		if count := rest / unit.size; count > 0 {
			out.WriteString(strconv.FormatInt(int64(count), 10) + unit.symbol)
			rest %= unit.size
		}
	}
	return out.String()
}

func closeApproval(approvals *approvalTable, pending *pendingApproval) (approvalVerdict, bool) {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	delete(approvals.m, pending.id)
	select {
	case verdict := <-pending.answer:
		return verdict, true
	default:
		return approvalVerdict{}, false
	}
}

func (t *approvalTable) denialReason(mode approval.Mode, req approval.Request) string {
	if mode == approval.ModePlan {
		return t.text("approval.plan_mode", requestParams(req))
	}
	return t.text("approval.policy_denied", requestParams(req))
}

func registerApproval(approvals *approvalTable, pending *pendingApproval) (func(), string) {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	if len(approvals.m) >= maxPendingApprovals {
		params := requestParams(pending.req)
		params["Limit"] = maxPendingApprovals
		return nil, approvals.text("approval.fleet_full", params)
	}
	mine := 0
	for _, other := range approvals.m {
		if other.session == pending.session {
			mine++
		}
	}
	if mine >= maxPendingPerSession {
		params := requestParams(pending.req)
		params["Session"] = pending.session
		params["Limit"] = maxPendingPerSession
		return nil, approvals.text("approval.session_full", params)
	}
	approvals.m[pending.id] = pending
	return func() {
		approvals.mu.Lock()
		delete(approvals.m, pending.id)
		approvals.mu.Unlock()
	}, ""
}

func (t *approvalTable) warnWaitTruncated(configured, bound time.Duration) {
	warnTo(t.messages, t.warn, t.lang(), "approve.wait_truncated", map[string]any{waitParam: configured.String(), boundParam: bound.Round(time.Second).String()})
}

func settleApproval(approvals *approvalTable, approvalID string, answer approvalAnswer) bool {
	approvals.mu.Lock()
	defer approvals.mu.Unlock()
	pending := approvals.m[approvalID]
	if pending == nil || !time.Now().Before(pending.deadline) {
		return false
	}
	select {
	case pending.answer <- approvalVerdict{decision: answer.decision(), reason: answer.reason, scope: answer.scope}:
		return true
	default:
		return false
	}
}

func (t *approvalTable) remember(sessions *sessionTable, pending *pendingApproval, scope approvalScope) {
	switch scope {
	case scopeSession:
		if driver := sessions.lookup(pending.session); driver != nil {
			driver.remember(pending.req)
			return
		}
		params := requestParams(pending.req)
		params["Session"] = pending.session
		warnTo(t.messages, t.warn, t.lang(), "approval.session_not_running", params)
	case scopeAlways:
		t.addRule(pending.req)
	case scopeOnce, scopeDeny:
	}
}

func (t *approvalTable) addRule(req approval.Request) {
	rule, ok := approval.RuleFor(req.Tool, req.Subject)
	if !ok {
		warnTo(t.messages, t.warn, t.lang(), "approval.no_exact_subject", requestParams(req))
		return
	}
	if t.rules == nil {
		warnTo(t.messages, t.warn, t.lang(), "approval.no_rule_store", requestParams(req))
		return
	}
	if err := t.rules.AddApprovalRule(rule.String()); err != nil {
		warnTo(t.messages, t.warn, t.lang(), "approval.rule_not_saved", map[string]any{"Request": describeRequest(req), "Rule": rule.String(), "Detail": err.Error()})
	}
}

func (s *sessionTable) verdict(session string, req approval.Request) (approval.Risk, bool) {
	if s == nil {
		return 0, false
	}
	driver := s.lookup(session)
	if driver == nil {
		return 0, false
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

func answerApprovalPick(approvals *approvalTable, session, value string) bool {
	if !strings.HasPrefix(value, approvalPickPrefix) {
		return false
	}
	approvalID, answer, ok := parseApprovalPick(value)
	if !ok {
		return true
	}
	answer.reason = approvals.operatorRefusal(answer.reason)
	if owner, known := approvalSession(approvals, approvalID); known && owner != session {
		return true
	}
	settleApproval(approvals, approvalID, answer)
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
	return approvalID, approvalAnswer{scope: scope, reason: reason}, true
}

func scopeNamed(word string) (approvalScope, bool) {
	for _, scope := range approvalScopes() {
		if scope.String() == word {
			return scope, true
		}
	}
	return 0, false
}

func (t *approvalTable) operatorRefusal(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	return t.text("approval.operator_refused", map[string]any{"Reason": reason})
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
	return rand.Text()
}

func describeRequest(req approval.Request) string {
	if req.Subject == "" {
		return req.Tool
	}
	return req.Tool + "(" + clip(req.Subject) + ")"
}

func requestParams(req approval.Request) map[string]any {
	return map[string]any{"Request": describeRequest(req)}
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
	scopes := approvalScopes()
	choices := make([]contracts.Choice, 0, len(scopes))
	for _, scope := range scopes {
		choices = append(choices, contracts.Choice{Label: scopeLabel(scope, req), Value: approvalPickPrefix + approvalID + ":" + scope.String()})
	}
	return choices
}

func scopeLabel(scope approvalScope, req approval.Request) string {
	if scope != scopeAlways {
		return scope.String()
	}
	rule, ok := approval.RuleFor(req.Tool, req.Subject)
	if !ok {
		return scope.String()
	}
	return scope.String() + " " + rule.Tool + "(" + rule.Pattern + ")"
}
