package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

func frenchApprovalTable() *approvalTable {
	return newApprovalTable(io.Discard).withMessages(newHostMessages(), fixedLanguageSource(i18n.French))
}

func expectRefusal(t *testing.T, got askResult, want string) {
	t.Helper()
	if got.decision != approval.Deny {
		t.Fatalf("decision %q, want deny", got.decision)
	}
	if got.reason != want {
		t.Fatalf("reason %q, want %q", got.reason, want)
	}
}

func TestAnOperatorRefusalWithoutReasonIsDeniedInFrench(t *testing.T) {
	approvals := frenchApprovalTable()
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, askPolicy(), approval.ModeAsk, time.Minute)
	pending := onlyPending(t, approvals)
	if !answerApprovalPick(approvals, "s1", approvalPickPrefix+pending.ID+":deny") {
		t.Fatal("the operator's pick was not taken")
	}
	expectRefusal(t, awaitAsk(t, out), "herrscher : Bash(git push) a été refusé par l'opérateur")
}

func TestAnOperatorRefusalWithAReasonIsSentToTheAgentInFrench(t *testing.T) {
	approvals := frenchApprovalTable()
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	out := askInBackground(approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, askPolicy(), approval.ModeAsk, time.Minute)
	pending := onlyPending(t, approvals)
	if !answerApprovalPick(approvals, "s1", approvalPickPrefix+pending.ID+":deny:utilise make clean") {
		t.Fatal("the operator's pick was not taken")
	}
	expectRefusal(t, awaitAsk(t, out), "herrscher : l'opérateur a refusé : utilise make clean")
}

func TestAPlanModeRefusalIsGivenInFrench(t *testing.T) {
	approvals := frenchApprovalTable()
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	decision, reason := askApproval(context.Background(), approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, askPolicy(), approval.ModePlan, time.Minute, 0)
	expectRefusal(t, askResult{decision: decision, reason: reason}, "herrscher : Bash(git push) a été refusé : cette session est en mode plan, qui ne fait que lire ; écris le plan, l'opérateur le valide avant toute écriture")
}

func TestAPolicyDenialIsGivenInFrench(t *testing.T) {
	approvals := frenchApprovalTable()
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	pol := approval.Policy{{Decision: approval.Deny, Tool: "Bash", Pattern: "sudo*"}}
	decision, reason := askApproval(context.Background(), approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "sudo rm"}, pol, approval.ModeAsk, time.Minute, 0)
	expectRefusal(t, askResult{decision: decision, reason: reason}, "herrscher : Bash(sudo rm) a été refusé par la politique")
}

func TestAFleetFullRefusalIsGivenInFrench(t *testing.T) {
	approvals := frenchApprovalTable()
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	for i := range maxPendingApprovals {
		id := strconv.Itoa(i)
		approvals.m[id] = &pendingApproval{id: id, session: "autre", answer: make(chan approvalVerdict, 1)}
	}
	decision, reason := askApproval(context.Background(), approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, askPolicy(), approval.ModeAsk, time.Minute, 0)
	want := fmt.Sprintf("herrscher : Bash(git push) a été refusé : %d appels d'outils de la flotte sont déjà en attente d'un humain, c'est la limite ; chaque réponse libère une place", maxPendingApprovals)
	expectRefusal(t, askResult{decision: decision, reason: reason}, want)
}

func TestASessionFullRefusalIsGivenInFrench(t *testing.T) {
	approvals := frenchApprovalTable()
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	for i := range maxPendingPerSession {
		id := strconv.Itoa(i)
		approvals.m[id] = &pendingApproval{id: id, session: "s1", answer: make(chan approvalVerdict, 1)}
	}
	decision, reason := askApproval(context.Background(), approvals, sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, askPolicy(), approval.ModeAsk, time.Minute, 0)
	want := fmt.Sprintf("herrscher : Bash(git push) a été refusé : la session « s1 » a déjà %d appels d'outils qui sont en attente d'un humain, c'est la limite ; attends leurs réponses, et demande moins de choses à la fois", maxPendingPerSession)
	expectRefusal(t, askResult{decision: decision, reason: reason}, want)
}

type failingRules struct{}

func (failingRules) AddApprovalRule(string) error {
	return errors.New("boom")
}

func frenchApprovalTableWriting(buf *bytes.Buffer) *approvalTable {
	return newApprovalTable(buf).withMessages(newHostMessages(), fixedLanguageSource(i18n.French))
}

func expectWarning(t *testing.T, buf *bytes.Buffer, want string) {
	t.Helper()
	if buf.String() != want+"\n" {
		t.Fatalf("warning %q, want %q", buf.String(), want+"\n")
	}
}

func TestASessionScopeWithoutADriverWarnsInFrench(t *testing.T) {
	var buf bytes.Buffer
	approvals := frenchApprovalTableWriting(&buf)
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	pending := &pendingApproval{id: "p1", session: "s1", req: approval.Request{Tool: "Bash", Subject: "git push"}}
	approvals.remember(sessions, pending, scopeSession)
	expectWarning(t, &buf, "approbations : Bash(git push) a été autorisé une fois : la session « s1 » ne tourne pas, elle ne peut donc pas retenir la réponse")
}

func TestAnAlwaysScopeWithoutAnExactSubjectWarnsInFrench(t *testing.T) {
	var buf bytes.Buffer
	approvals := frenchApprovalTableWriting(&buf).withRules(failingRules{})
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	pending := &pendingApproval{id: "p1", session: "s1", req: approval.Request{Tool: "Bash", Subject: "git push*"}}
	approvals.remember(sessions, pending, scopeAlways)
	expectWarning(t, &buf, "approbations : Bash(git push*) a été autorisé une fois : il n'a pas de sujet exact qu'une règle puisse nommer sans couvrir plus que cette demande")
}

func TestAnAlwaysScopeWithoutARuleStoreWarnsInFrench(t *testing.T) {
	var buf bytes.Buffer
	approvals := frenchApprovalTableWriting(&buf)
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	pending := &pendingApproval{id: "p1", session: "s1", req: approval.Request{Tool: "Bash", Subject: "git push"}}
	approvals.remember(sessions, pending, scopeAlways)
	expectWarning(t, &buf, "approbations : Bash(git push) a été autorisé une fois : aucun magasin de règles n'est attaché, donc aucune règle n'a été écrite")
}

func TestAnAlwaysScopeWhoseRuleIsNotSavedWarnsInFrench(t *testing.T) {
	var buf bytes.Buffer
	approvals := frenchApprovalTableWriting(&buf).withRules(failingRules{})
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	req := approval.Request{Tool: "Bash", Subject: "git push"}
	rule, ok := approval.RuleFor(req.Tool, req.Subject)
	if !ok {
		t.Fatal("the subject must have an exact rule")
	}
	pending := &pendingApproval{id: "p1", session: "s1", req: req}
	approvals.remember(sessions, pending, scopeAlways)
	expectWarning(t, &buf, fmt.Sprintf("approbations : Bash(git push) a été autorisé une fois : la règle « %s » n'a pas été enregistrée : boom", rule.String()))
}

func TestASmartModeWithoutAModelWarnsInFrench(t *testing.T) {
	var buf bytes.Buffer
	approvals := frenchApprovalTableWriting(&buf)
	approvals.judge = func(context.Context, approval.Request) (approval.Risk, error) {
		return 0, errNoRiskModel
	}
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	approvals.rated(context.Background(), sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, approval.Deny)
	expectWarning(t, &buf, "approbations : le mode smart n'a pas de modèle pour évaluer Bash(git push), il est donc décidé comme en mode demande")
}

func TestASmartModeThatCannotRateWarnsInFrench(t *testing.T) {
	var buf bytes.Buffer
	approvals := frenchApprovalTableWriting(&buf)
	approvals.judge = func(context.Context, approval.Request) (approval.Risk, error) {
		return 0, errors.New("boom")
	}
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	approvals.rated(context.Background(), sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, approval.Deny)
	expectWarning(t, &buf, "approbations : le mode smart n'a pas pu évaluer Bash(git push) (boom), il est donc décidé comme en mode demande")
}

func TestASmartModeWithoutAJudgeWarnsInFrench(t *testing.T) {
	var buf bytes.Buffer
	approvals := frenchApprovalTableWriting(&buf)
	approvals.judge = nil
	sessions := newSessionTable(newHostMessages(), clitext.NewMessages())
	approvals.rated(context.Background(), sessions, "s1", approval.Request{Tool: "Bash", Subject: "git push"}, approval.Deny)
	expectWarning(t, &buf, "approbations : le mode smart n'a pas de modèle pour évaluer Bash(git push), il est donc décidé comme en mode demande")
}
