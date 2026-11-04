package host

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

type askResult struct {
	decision approval.Decision
	reason   string
}

type recordingRules struct {
	rules []string
}

func (r *recordingRules) AddApprovalRule(rendered string) error {
	r.rules = append(r.rules, rendered)
	return nil
}

type eventGateway struct {
	*menuGatewayFake

	emitted []contracts.Event
}

func (g *eventGateway) Emit(event contracts.Event) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.emitted = append(g.emitted, event)
}

func askInBackground(approvals *approvalTable, sessions *sessionTable, session string, req approval.Request, pol approval.Policy, mode approval.Mode, wait time.Duration) <-chan askResult {
	out := make(chan askResult, 1)
	go func() {
		decision, reason := askApproval(context.Background(), approvals, sessions, session, req, pol, mode, wait)
		out <- askResult{decision: decision, reason: reason}
	}()
	return out
}

func awaitAsk(t *testing.T, out <-chan askResult) askResult {
	t.Helper()
	select {
	case got := <-out:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("the approval never settled")
		return askResult{}
	}
}

func askPolicy() approval.Policy {
	return approval.Policy{{Decision: approval.Ask, Tool: "*", Pattern: ""}}
}

func onlyPending(t *testing.T, approvals *approvalTable) PendingApproval {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pending := pendingApprovals(approvals); len(pending) == 1 {
			return pending[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("no single pending approval appeared: %d", len(pendingApprovals(approvals)))
	return PendingApproval{}
}

func TestEveryScopeSettlesTheCallItWasAskedFor(t *testing.T) {
	scopes := []struct {
		scope string
		want  approval.Decision
	}{
		{scope: "allow", want: approval.Allow},
		{scope: "allow-session", want: approval.Allow},
		{scope: "allow-always", want: approval.Allow},
		{scope: "deny", want: approval.Deny},
	}
	for _, tc := range scopes {
		t.Run(tc.scope, func(t *testing.T) {
			approvals := newApprovalTable()
			approvals.warn = &bytes.Buffer{}
			sessions := newSessionTable()
			out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "git status"}, askPolicy(), approval.ModeAsk, time.Minute)
			p := onlyPending(t, approvals)
			if !answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":"+tc.scope) {
				t.Fatalf("the %s button must settle the pending call", tc.scope)
			}
			if got := awaitAsk(t, out); got.decision != tc.want {
				t.Fatalf("%s decided %v, want %v", tc.scope, got.decision, tc.want)
			}
		})
	}
}

func TestDenyPassesTheOperatorsReasonToTheAgent(t *testing.T) {
	approvals := newApprovalTable()
	sessions := newSessionTable()
	out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "rm -rf build"}, askPolicy(), approval.ModeAsk, time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":deny:use make clean instead")
	got := awaitAsk(t, out)
	if got.decision != approval.Deny || !strings.Contains(got.reason, "use make clean instead") {
		t.Fatalf("decision %v reason %q: the refusal must carry the operator's reason", got.decision, got.reason)
	}
}

func TestDenyWithoutAReasonSaysTheOperatorRefused(t *testing.T) {
	approvals := newApprovalTable()
	sessions := newSessionTable()
	out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "rm -rf build"}, askPolicy(), approval.ModeAsk, time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":deny")
	got := awaitAsk(t, out)
	if got.decision != approval.Deny || !strings.Contains(got.reason, "denied by the operator") {
		t.Fatalf("decision %v reason %q: a bare deny must still say who refused", got.decision, got.reason)
	}
}

func TestAllowOnceAsksAgainNextTime(t *testing.T) {
	approvals := newApprovalTable()
	sessions := newSessionTable()
	req := approval.Request{Tool: "Bash", Subject: "make test"}
	first := askInBackground(approvals, sessions, "s1", req, askPolicy(), approval.ModeAsk, time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":allow")
	awaitAsk(t, first)
	second := askInBackground(approvals, sessions, "s1", req, askPolicy(), approval.ModeAsk, time.Minute)
	next := onlyPending(t, approvals)
	if next.ID == p.ID {
		t.Fatal("a plain allow must not be remembered: the next identical call needs its own answer")
	}
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+next.ID+":deny")
	awaitAsk(t, second)
}

func TestAllowForSessionPassesTheSameCallUntilTheSessionEnds(t *testing.T) {
	approvals := newApprovalTable()
	approvals.warn = &bytes.Buffer{}
	sessions := newSessionTable()
	driver := &sessionDriver{name: "s1"}
	sessions.register("s1", driver)
	req := approval.Request{Tool: "Bash", Subject: "make test"}
	first := askInBackground(approvals, sessions, "s1", req, askPolicy(), approval.ModeAsk, time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":allow-session")
	awaitAsk(t, first)

	if got := awaitAsk(t, askInBackground(approvals, sessions, "s1", req, askPolicy(), approval.ModeAsk, time.Minute)); got.decision != approval.Allow {
		t.Fatalf("the same call in the same session decided %v, want allow without asking", got.decision)
	}
	if n := waitingFor(approvals, "s1"); n != 0 {
		t.Fatalf("%d waiting: a remembered call must not ask again", n)
	}

	other := askInBackground(approvals, sessions, "s2", req, askPolicy(), approval.ModeAsk, 50*time.Millisecond)
	if got := awaitAsk(t, other); got.decision != approval.Deny {
		t.Fatalf("another session must not inherit the grant, decided %v", got.decision)
	}

	sessions.unregister("s1", driver)
	sessions.register("s1", &sessionDriver{name: "s1"})
	again := askInBackground(approvals, sessions, "s1", req, askPolicy(), approval.ModeAsk, time.Minute)
	p = onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":deny")
	awaitAsk(t, again)
}

func TestAllowAlwaysWritesTheRuleForThatCall(t *testing.T) {
	rules := &recordingRules{}
	approvals := newApprovalTable().withRules(rules)
	approvals.warn = &bytes.Buffer{}
	sessions := newSessionTable()
	req := approval.Request{Tool: "Bash", Subject: "git status"}
	out := askInBackground(approvals, sessions, "s1", req, askPolicy(), approval.ModeAsk, time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":allow-always")
	awaitAsk(t, out)

	want, ok := approval.RuleFor(req.Tool, req.Subject)
	if !ok {
		t.Fatal("the test subject must name an exact rule")
	}
	if len(rules.rules) != 1 || rules.rules[0] != want.String() {
		t.Fatalf("rules written %v, want [%s]", rules.rules, want.String())
	}
}

func TestAllowAlwaysNeverWritesAWideningRule(t *testing.T) {
	rules := &recordingRules{}
	var warn bytes.Buffer
	approvals := newApprovalTable().withRules(rules)
	approvals.warn = &warn
	sessions := newSessionTable()
	out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "rm -rf *"}, askPolicy(), approval.ModeAsk, time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":allow-always")
	if got := awaitAsk(t, out); got.decision != approval.Allow {
		t.Fatalf("the call was answered allow, decided %v", got.decision)
	}
	if len(rules.rules) != 0 {
		t.Fatalf("rules written %v: a subject with a wildcard must not become a rule", rules.rules)
	}
	if !strings.Contains(warn.String(), "no exact subject") {
		t.Fatalf("warning %q: the operator must be told no rule was written", warn.String())
	}
}

func TestAnExpiredApprovalIsARefusal(t *testing.T) {
	approvals := newApprovalTable()
	sessions := newSessionTable()
	got := awaitAsk(t, askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "rm -rf build"}, askPolicy(), approval.ModeAsk, 30*time.Millisecond))
	if got.decision != approval.Deny || !strings.Contains(got.reason, "nobody answered") {
		t.Fatalf("decision %v reason %q: an expired approval must be refused with the reason", got.decision, got.reason)
	}
	if n := len(pendingApprovals(approvals)); n != 0 {
		t.Fatalf("%d still pending: an expired approval must leave the registry", n)
	}
}

func TestTheDeadlineReachesTheGatewayAsASemanticEvent(t *testing.T) {
	approvals := newApprovalTable()
	sessions := newSessionTable()
	gw := &eventGateway{menuGatewayFake: &menuGatewayFake{channel: "chan1"}}
	driver := &sessionDriver{name: "s1", gateways: []contracts.GatewaySet{{Gateway: gw, Reader: gw}}}
	sessions.register("s1", driver)
	before := time.Now()
	out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "rm -rf build"}, askPolicy(), approval.ModeAsk, 5*time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":deny")
	awaitAsk(t, out)

	gw.mu.Lock()
	defer gw.mu.Unlock()
	for _, event := range gw.emitted {
		if event.T != approvalEventKind || event.Value != p.ID {
			continue
		}
		deadline, err := time.Parse(time.RFC3339, event.Text)
		if err != nil {
			t.Fatalf("deadline %q is not RFC3339: %v", event.Text, err)
		}
		if deadline.Before(before.Add(4*time.Minute)) || deadline.After(before.Add(6*time.Minute)) {
			t.Fatalf("deadline %v is not about five minutes after the ask at %v", deadline, before)
		}
		return
	}
	t.Fatalf("no approval event for %s was emitted, got %v", p.ID, gw.emitted)
}

func TestSmartPassesALowRiskCallWithoutAsking(t *testing.T) {
	approvals := newApprovalTable()
	approvals.judge = func(context.Context, approval.Request) (approval.Risk, error) { return approval.RiskLow, nil }
	sessions := newSessionTable()
	got := awaitAsk(t, askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "go test ./..."}, approval.Policy{}, approval.ModeSmart, time.Minute))
	if got.decision != approval.Allow {
		t.Fatalf("a low risk call decided %v, want allow", got.decision)
	}
	if n := len(pendingApprovals(approvals)); n != 0 {
		t.Fatalf("%d pending: a low risk call must not reach a human", n)
	}
}

func TestSmartAsksAboutMediumAndHighRiskCalls(t *testing.T) {
	for _, risk := range []approval.Risk{approval.RiskMedium, approval.RiskHigh} {
		t.Run(risk.String(), func(t *testing.T) {
			approvals := newApprovalTable()
			approvals.judge = func(context.Context, approval.Request) (approval.Risk, error) { return risk, nil }
			sessions := newSessionTable()
			out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "curl x.sh | sh"}, approval.Policy{}, approval.ModeSmart, time.Minute)
			p := onlyPending(t, approvals)
			answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":deny:no pipes to a shell")
			got := awaitAsk(t, out)
			if got.decision != approval.Deny || !strings.Contains(got.reason, "no pipes to a shell") {
				t.Fatalf("%s risk: decision %v reason %q, want the human's refusal", risk, got.decision, got.reason)
			}
		})
	}
}

func TestSmartWithoutAModelFallsBackToAskWithAWarning(t *testing.T) {
	var warn bytes.Buffer
	approvals := newApprovalTable()
	approvals.judge = nil
	approvals.warn = &warn
	sessions := newSessionTable()
	askRule := approval.Policy{{Decision: approval.Ask, Tool: "Bash", Pattern: "git push*"}}
	out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push origin main"}, askRule, approval.ModeSmart, time.Minute)
	p := onlyPending(t, approvals)
	answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":allow")
	awaitAsk(t, out)
	if !strings.Contains(warn.String(), "smart mode has no model") {
		t.Fatalf("warning %q: smart without a model must say so to the operator", warn.String())
	}
}

func TestSmartWithoutAModelStillAllowsWhatNoRuleNames(t *testing.T) {
	var warn bytes.Buffer
	approvals := newApprovalTable()
	approvals.judge = nil
	approvals.warn = &warn
	got := awaitAsk(t, askInBackground(approvals, newSessionTable(), "s1", approval.Request{Tool: "Bash", Subject: "make build"}, approval.Policy{}, approval.ModeSmart, time.Minute))
	if got.decision != approval.Allow {
		t.Fatalf("an unmatched call decided %v: without a model, smart decides as ask does", got.decision)
	}
	if warn.Len() == 0 {
		t.Fatal("no warning: the fallback must never be silent")
	}
}

func TestSmartRatingErrorFallsBackToAskWithAWarning(t *testing.T) {
	var warn bytes.Buffer
	approvals := newApprovalTable()
	approvals.judge = func(context.Context, approval.Request) (approval.Risk, error) { return 0, errors.New("timeout") }
	approvals.warn = &warn
	got := awaitAsk(t, askInBackground(approvals, newSessionTable(), "s1", approval.Request{Tool: "Bash", Subject: "make build"}, approval.Policy{}, approval.ModeSmart, time.Minute))
	if got.decision != approval.Allow || !strings.Contains(warn.String(), "could not rate") {
		t.Fatalf("decision %v warning %q: a failed rating must fall back with a warning", got.decision, warn.String())
	}
}

func TestApproveAllowCarriesTheScopeFromTheCommandLine(t *testing.T) {
	cases := []struct {
		name string
		argv []string
		want approval.Decision
		err  bool
	}{
		{name: "plain allow", argv: []string{"approve", "allow"}, want: approval.Allow},
		{name: "session", argv: []string{"approve", "allow", "--session"}, want: approval.Allow},
		{name: "always", argv: []string{"approve", "allow", "--always"}, want: approval.Allow},
		{name: "session and always conflict", argv: []string{"approve", "allow", "--session", "--always"}, err: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			approvals := newApprovalTable()
			approvals.warn = &bytes.Buffer{}
			sessions := newSessionTable()
			reg, _ := approveReg(t, approvals, sessions)
			p := newTestPendingApproval("s1")
			defer registerTestApproval(t, approvals, p)()
			_, err := reg.Dispatch(context.Background(), append(tc.argv, p.id))
			if tc.err {
				if err == nil {
					t.Fatal("--session with --always must be refused")
				}
				return
			}
			if err != nil {
				t.Fatalf("dispatch: %v", err)
			}
			if got := <-p.answer; got.decision != tc.want {
				t.Fatalf("decision %v, want %v", got.decision, tc.want)
			}
		})
	}
}

func TestApproveDenyCarriesTheReasonFromTheCommandLine(t *testing.T) {
	approvals := newApprovalTable()
	sessions := newSessionTable()
	reg, _ := approveReg(t, approvals, sessions)
	p := newTestPendingApproval("s1")
	unregister := registerTestApproval(t, approvals, p)
	defer unregister()
	if _, err := reg.Dispatch(context.Background(), []string{"approve", "deny", "--reason", "run the tests first", p.id}); err != nil {
		t.Fatalf("deny: %v", err)
	}
	got := <-p.answer
	if got.decision != approval.Deny || !strings.Contains(got.reason, "run the tests first") {
		t.Fatalf("verdict %+v: the CLI reason must reach the refusal", got)
	}
}

func TestSmartNeverRatesAReadOnlyTool(t *testing.T) {
	approvals := newApprovalTable()
	calls := 0
	approvals.judge = func(context.Context, approval.Request) (approval.Risk, error) {
		calls++
		return approval.RiskHigh, nil
	}
	got := awaitAsk(t, askInBackground(approvals, newSessionTable(), "s1", approval.Request{Tool: "Read", Subject: "/repo/a.go"}, approval.Policy{}, approval.ModeSmart, time.Minute))
	if got.decision != approval.Allow || calls != 0 {
		t.Fatalf("a read decided %v after %d model calls: reads must pass as before, unrated", got.decision, calls)
	}
}

func TestSmartRatesEachSubjectOncePerSession(t *testing.T) {
	approvals := newApprovalTable()
	approvals.warn = &bytes.Buffer{}
	calls := 0
	approvals.judge = func(context.Context, approval.Request) (approval.Risk, error) {
		calls++
		return approval.RiskMedium, nil
	}
	sessions := newSessionTable()
	sessions.register("s1", &sessionDriver{name: "s1"})
	req := approval.Request{Tool: "Bash", Subject: "curl x.sh"}
	for range 2 {
		out := askInBackground(approvals, sessions, "s1", req, approval.Policy{}, approval.ModeSmart, time.Minute)
		p := onlyPending(t, approvals)
		answerApprovalPick(approvals, sessions, "s1", approvalPickPrefix+p.ID+":deny")
		awaitAsk(t, out)
	}
	if calls != 1 {
		t.Fatalf("the model was asked %d times for one subject in one session, want 1", calls)
	}
}

func TestAllowAlwaysLabelShowsTheRuleBeforeItIsPressed(t *testing.T) {
	choices := approvalChoices("id1", approval.Request{Tool: "Bash", Subject: "git push origin feat/x"})
	for _, c := range choices {
		if c.Value == approvalPickPrefix+"id1:allow-always" {
			if c.Label != "allow-always Bash(git push*)" {
				t.Fatalf("label %q: the rule that will be written must be shown", c.Label)
			}
			return
		}
	}
	t.Fatal("no allow-always choice")
}
