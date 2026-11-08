package host

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/accounts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

type limitRead struct {
	used float64
	err  error
}

func readsFrom(reads map[contracts.ProfileName]limitRead) contracts.LimitReader {
	return func(_ context.Context, access contracts.ProbeAccess) ([]contracts.Limit, error) {
		read, ok := reads[access.Account]
		switch {
		case !ok:
			return nil, errors.New("no reading for the account")
		case read.err != nil:
			return nil, read.err
		}
		return []contracts.Limit{{UsedPercent: read.used}}, nil
	}
}

func newFallbackSource(t *testing.T, read contracts.LimitReader, profiles ...string) *usageSource {
	t.Helper()
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	contracts.Default = contracts.Registry{}
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "claude", Category: contracts.CategoryBackend},
		Limits:   read,
	})
	store := accounts.Store{Root: t.TempDir()}
	for _, name := range profiles {
		if _, err := store.Create("claude", contracts.ProfileName(name)); err != nil {
			t.Fatalf("create profile %s: %v", name, err)
		}
	}
	now := time.Date(2026, time.November, 9, 12, 0, 0, 0, time.UTC)
	return &usageSource{
		accounts: store,
		limits:   &usage.LimitsCache{Now: func() time.Time { return now }},
	}
}

func TestBalanceMovesANewSessionOffASpentAccount(t *testing.T) {
	source := newFallbackSource(t, readsFrom(map[contracts.ProfileName]limitRead{
		"work":  {used: 100},
		"perso": {used: 10},
	}), "work", "perso")
	fallback := newAccountFallback(source, nil)

	got := fallback.Balance(t.Context(), "claude", "work")
	want := usage.Choice{Outcome: usage.ChoiceSwitched, From: "work", To: "perso"}
	if got != want {
		t.Fatalf("Balance = %+v, want %+v", got, want)
	}
}

func TestBalanceKeepsAnAccountWhoseLimitsCannotBeRead(t *testing.T) {
	source := newFallbackSource(t, readsFrom(map[contracts.ProfileName]limitRead{
		"work":  {err: errors.New("vendor unreachable")},
		"perso": {used: 10},
	}), "work", "perso")
	fallback := newAccountFallback(source, nil)

	got := fallback.Balance(t.Context(), "claude", "work")
	if got.Outcome != usage.ChoiceKept || got.From != "work" {
		t.Fatalf("Balance = %+v, want the planned account kept", got)
	}
}

func TestBalanceReportsNoQuotaWhenEveryAccountIsSpent(t *testing.T) {
	source := newFallbackSource(t, readsFrom(map[contracts.ProfileName]limitRead{
		"work":  {used: 100},
		"perso": {used: 100},
	}), "work", "perso")
	fallback := newAccountFallback(source, nil)

	got := fallback.Balance(t.Context(), "claude", "work")
	if got.Outcome != usage.ChoiceNoQuota || got.From != "work" {
		t.Fatalf("Balance = %+v, want no quota with the planned account named", got)
	}
}

func TestBalanceNeverTakesAnAccountOfAnotherVendor(t *testing.T) {
	saved := contracts.Default
	t.Cleanup(func() { contracts.Default = saved })
	source := newFallbackSource(t, readsFrom(map[contracts.ProfileName]limitRead{
		"work": {used: 100},
	}), "work")
	contracts.Default.Register(contracts.Plugin{
		Manifest: contracts.Manifest{Kind: "codex", Category: contracts.CategoryBackend},
		Limits:   readsFrom(map[contracts.ProfileName]limitRead{"perso": {used: 0}}),
	})
	if _, err := (accounts.Store{Root: source.accounts.Root}).Create("codex", "perso"); err != nil {
		t.Fatalf("create codex profile: %v", err)
	}
	fallback := newAccountFallback(source, nil)

	got := fallback.Balance(t.Context(), "claude", "work")
	if got.Outcome != usage.ChoiceNoQuota {
		t.Fatalf("Balance = %+v, want no quota: the codex account is another vendor", got)
	}
}

func TestBalanceGivesUpAtItsTimeoutAndKeepsTheAccount(t *testing.T) {
	block := func(ctx context.Context, _ contracts.ProbeAccess) ([]contracts.Limit, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	source := newFallbackSource(t, block, "work", "perso")
	source.limits.Timeout = time.Hour
	fallback := newAccountFallback(source, nil)
	fallback.timeout = 10 * time.Millisecond

	got := fallback.Balance(t.Context(), "claude", "work")
	if got.Outcome != usage.ChoiceKept {
		t.Fatalf("Balance = %+v, want the planned account kept after the timeout", got)
	}
}

func TestDefaultFirstPutsTheDefaultProfileFirst(t *testing.T) {
	targets := []usage.Target{
		{Vendor: "claude", Account: "perso"},
		{Vendor: "claude", Account: "work", Default: true},
	}
	got := defaultFirst(targets)
	if got[0].Account != "work" || got[1].Account != "perso" {
		t.Fatalf("order = %q, %q; want the default profile first", got[0].Account, got[1].Account)
	}
}

func TestDefaultFirstMakesTheNativeLoginTheDefaultWhenNoProfileIs(t *testing.T) {
	targets := []usage.Target{
		{Vendor: "claude", Account: "perso"},
		{Vendor: "claude", Account: ""},
	}
	got := defaultFirst(targets)
	if got[0].Account != "" || !got[0].Default || got[1].Default {
		t.Fatalf("targets = %+v, want the native login first and marked default", got)
	}
}

type sendRecorder struct {
	labeledAdmin

	sent []string
}

func (r *sendRecorder) Send(_ context.Context, _, content string) error {
	r.sent = append(r.sent, content)
	return nil
}

func TestNoticeReachesTheGatewayOfTheHomeAsATypedNotice(t *testing.T) {
	gateway := newNoticeGateway()
	fallback := newAccountFallback(nil, []Deps{{Gateway: gateway, Admin: labeledAdmin{"chat"}}})
	notice := contracts.Notice{Code: contracts.CodeAccountFallback, Args: []contracts.Arg{contracts.Text("From", "work"), contracts.Text("To", "perso")}}

	fallback.Notify(t.Context(), state.HomeRef{ID: "home", Type: "forum"}, "chan-1", i18n.English, notice)

	record := awaitNotice(t, gateway)
	if record.conv.ID != "chan-1" || record.notice.Code != contracts.CodeAccountFallback || record.lang != i18n.English {
		t.Fatalf("delivered %+v, want the notice for chan-1 in English", record)
	}
}

func TestNoticeFallsBackToRenderedTextWhenTheGatewayHasNoSink(t *testing.T) {
	admin := &sendRecorder{}
	fallback := newAccountFallback(nil, []Deps{{Gateway: fakeGateway{}, Admin: admin}})
	notice := contracts.Notice{Code: contracts.CodeAccountFallback, Args: []contracts.Arg{contracts.Text("From", "work"), contracts.Text("To", "perso")}}

	fallback.Notify(t.Context(), state.HomeRef{ID: "home", Type: "forum"}, "chan-1", i18n.English, notice)

	if len(admin.sent) != 1 || !strings.Contains(admin.sent[0], "work") || !strings.Contains(admin.sent[0], "perso") {
		t.Fatalf("sent %q, want one text naming both accounts", admin.sent)
	}
}

type fallbackPostedSink struct {
	fakeGateway

	calls int
}

func (s *fallbackPostedSink) Notify(context.Context, contracts.Conversation, i18n.Language, contracts.Notice) (contracts.MessageID, error) {
	s.calls++
	return "", contracts.ErrNoticeFallbackPosted
}

func TestNoticeIsNotResentWhenTheGatewayPostedItsFallback(t *testing.T) {
	gateway := &fallbackPostedSink{}
	admin := &sendRecorder{}
	fallback := newAccountFallback(nil, []Deps{{Gateway: gateway, Admin: admin}})
	notice := contracts.Notice{Code: contracts.CodeAccountNoQuota, Args: []contracts.Arg{contracts.Text("Vendor", "claude"), contracts.Text("Account", "work")}}

	fallback.Notify(t.Context(), state.HomeRef{ID: "home", Type: "forum"}, "chan-1", i18n.English, notice)

	if gateway.calls != 1 || len(admin.sent) != 0 {
		t.Fatalf("notify calls %d, sent %q: the gateway's fallback counts as delivered, nothing is sent again", gateway.calls, admin.sent)
	}
}
