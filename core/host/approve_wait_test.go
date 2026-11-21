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

func TestAnUnansweredApprovalWaitIsWrittenForAHuman(t *testing.T) {
	table := newApprovalTable(io.Discard).withMessages(newHostMessages(), fixedLanguageSource(i18n.French))
	pending := &pendingApproval{req: approval.Request{Tool: "Bash", Subject: "git push"}, answer: make(chan approvalVerdict, 1)}
	_, reason := table.awaitVerdict(context.Background(), pending, 1100*time.Millisecond)
	const want = "herrscher : personne n'a répondu dans 1s, donc Bash(git push) est refusé"
	if reason != want {
		t.Fatalf("reason %q, want %q", reason, want)
	}
}

func TestAWaitIsWrittenForAHuman(t *testing.T) {
	cases := []struct {
		wait time.Duration
		want string
	}{
		{0, "0ms"},
		{50 * time.Millisecond, "50ms"},
		{999 * time.Millisecond, "999ms"},
		{time.Second, "1s"},
		{1100 * time.Millisecond, "1s"},
		{1500 * time.Millisecond, "2s"},
		{59 * time.Second, "59s"},
		{59*time.Second + 600*time.Millisecond, "1m"},
		{90 * time.Second, "1m30s"},
		{810 * time.Second, "13m30s"},
		{59*time.Minute + 59*time.Second, "59m59s"},
		{59*time.Minute + 59*time.Second + 600*time.Millisecond, "1h"},
		{time.Hour, "1h"},
		{90 * time.Minute, "1h30m"},
		{2*time.Hour + 5*time.Second, "2h5s"},
		{3*time.Hour + 2*time.Minute + time.Second, "3h2m1s"},
	}
	for _, c := range cases {
		if got := humanWait(c.wait); got != c.want {
			t.Errorf("humanWait(%v) = %q, want %q", c.wait, got, c.want)
		}
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
