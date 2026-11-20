package host

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/approval"
)

func TestAnUnansweredApprovalGivesItsDenialInTheOperatorsLanguage(t *testing.T) {
	table := newApprovalTable(io.Discard).withMessages(newHostMessages(), fixedLanguageSource(i18n.French))
	pending := &pendingApproval{req: approval.Request{Tool: "Bash", Subject: "git push"}, answer: make(chan approvalVerdict, 1)}
	_, reason := table.awaitVerdict(context.Background(), pending, 50*time.Millisecond)
	const want = "herrscher : personne n'a répondu dans 50ms, donc Bash(git push) est refusé"
	if reason != want {
		t.Fatalf("reason %q, want %q", reason, want)
	}
}

func TestACancelledApprovalWaitGivesItsDenialInTheOperatorsLanguage(t *testing.T) {
	table := newApprovalTable(io.Discard).withMessages(newHostMessages(), fixedLanguageSource(i18n.French))
	pending := &pendingApproval{req: approval.Request{Tool: "Bash", Subject: "git push"}, answer: make(chan approvalVerdict, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, reason := table.awaitVerdict(ctx, pending, time.Hour)
	const want = "herrscher : l'attente d'approbation a pris fin, donc Bash(git push) est refusé"
	if reason != want {
		t.Fatalf("reason %q, want %q", reason, want)
	}
}
