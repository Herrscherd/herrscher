package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

type fakeAccountFallback struct {
	reads    []usage.AccountLimits
	balanced []string
	notices  []contracts.Notice
}

func (f *fakeAccountFallback) Balance(_ context.Context, _, planned string) usage.Choice {
	f.balanced = append(f.balanced, planned)
	return usage.ChooseAccount(planned, f.reads)
}

func (f *fakeAccountFallback) Notify(_ context.Context, _ state.HomeRef, _ string, _ i18n.Language, notice contracts.Notice) {
	f.notices = append(f.notices, notice)
}

func newFallbackHandler(t *testing.T, reads ...usage.AccountLimits) (*Handler, *fakeAccountFallback, *state.State) {
	t.Helper()
	h, _, _, _, _, st := newTestHandler(t, "forum")
	st.Home = state.HomeRef{ID: "home", Type: "forum"}
	h.validateAccount = func(_, _ string) error { return nil }
	fallback := &fakeAccountFallback{reads: reads}
	h.SetAccountFallback(fallback)
	return h, fallback, st
}

func readOf(account string, usedPercent float64) usage.AccountLimits {
	return usage.AccountLimits{
		Vendor:  "claude",
		Account: account,
		Status:  usage.LimitsReady,
		Limits:  []contracts.Limit{{UsedPercent: usedPercent}},
	}
}

func spentDefaultWork() usage.AccountLimits {
	read := readOf("work", 100)
	read.Default = true
	return read
}

func renderedNotice(t *testing.T, notice contracts.Notice) string {
	t.Helper()
	text, err := contracts.NewRenderer().Render(i18n.English, notice)
	if err != nil {
		t.Fatalf("render %s: %v", notice.Code.Key(), err)
	}
	return text
}

func TestNewSessionStartsOnTheNextAccountWhenTheDefaultOneIsSpent(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 10))

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, ok := st.FindSession("fresh")
	if !ok || sess.Account != "perso" {
		t.Fatalf("session account = %q (found %v), want perso", sess.Account, ok)
	}
	if len(fallback.notices) != 1 || fallback.notices[0].Code != contracts.CodeAccountFallback {
		t.Fatalf("notices = %+v, want one account fallback notice", fallback.notices)
	}
	if text := renderedNotice(t, fallback.notices[0]); !strings.Contains(text, "work") || !strings.Contains(text, "perso") {
		t.Fatalf("rendered notice %q does not name both accounts", text)
	}
}

func TestWithoutTheFallbackPortTheAccountIsUntouched(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "forum")
	st.Home = state.HomeRef{ID: "home", Type: "forum"}
	h.validateAccount = func(_, _ string) error { return nil }

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "account", "work")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "work" {
		t.Fatalf("session account = %q, want work kept when the fallback is off", sess.Account)
	}
}

func TestNoQuotaKeepsThePlannedAccountAndSaysSo(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 100))

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" {
		t.Fatalf("session account = %q, want the planned default kept", sess.Account)
	}
	if len(fallback.notices) != 1 || fallback.notices[0].Code != contracts.CodeAccountNoQuota {
		t.Fatalf("notices = %+v, want one no-quota notice", fallback.notices)
	}
	if text := renderedNotice(t, fallback.notices[0]); !strings.Contains(text, "work") {
		t.Fatalf("rendered notice %q does not name the kept account", text)
	}
}

func TestUnreadableLimitsKeepThePlannedAccountSilently(t *testing.T) {
	unavailable := usage.AccountLimits{Vendor: "claude", Account: "work", Default: true, Status: usage.LimitsUnavailable}
	h, fallback, st := newFallbackHandler(t, unavailable, readOf("perso", 10))

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" || len(fallback.notices) != 0 {
		t.Fatalf("account %q, notices %+v: want the planned default kept with no notice", sess.Account, fallback.notices)
	}
}

func TestUnavailableCandidateKeepsThePlannedAccountWithoutANoQuotaNotice(t *testing.T) {
	unavailable := usage.AccountLimits{Vendor: "claude", Account: "perso", Status: usage.LimitsUnavailable}
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), unavailable)

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" || len(fallback.notices) != 0 {
		t.Fatalf("account %q, notices %+v: want the planned default kept and no no-quota notice", sess.Account, fallback.notices)
	}
}

func TestAccountWithoutAWindowIsNeverAReplacement(t *testing.T) {
	bare := usage.AccountLimits{Vendor: "claude", Account: "perso", Status: usage.LimitsReady}
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), bare)

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" || len(fallback.notices) != 0 {
		t.Fatalf("account %q, notices %+v: want the planned default kept, perso has no proven quota", sess.Account, fallback.notices)
	}
}

func TestScopedWindowAtZeroPercentDoesNotMoveTheAccount(t *testing.T) {
	scopedSpent := usage.AccountLimits{
		Vendor:  "claude",
		Account: "work",
		Default: true,
		Status:  usage.LimitsReady,
		Limits:  []contracts.Limit{{Scope: "opus", UsedPercent: 100}, {UsedPercent: 20}},
	}
	h, fallback, st := newFallbackHandler(t, scopedSpent, readOf("perso", 10))

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" || len(fallback.notices) != 0 {
		t.Fatalf("account %q, notices %+v: want the planned default kept with no notice", sess.Account, fallback.notices)
	}
}

func TestExplicitAccountIsNeverMovedNorAnnounced(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, readOf("work", 100), readOf("perso", 10))

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "account", "work")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "work" {
		t.Fatalf("session account = %q, want the explicit work kept", sess.Account)
	}
	if len(fallback.balanced) != 0 || len(fallback.notices) != 0 {
		t.Fatalf("balanced %v, notices %+v: an explicit account is never balanced nor announced", fallback.balanced, fallback.notices)
	}
}

func TestExplicitSpentAccountIsNeverMovedNorAnnounced(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, readOf("work", 100))

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "account", "work")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "work" || len(fallback.balanced) != 0 || len(fallback.notices) != 0 {
		t.Fatalf("account %q, balanced %v, notices %+v: a spent explicit account is kept silently", sess.Account, fallback.balanced, fallback.notices)
	}
}

func TestRemoteHostAccountIsNeverMoved(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 10))
	h.SetHosts(&fakeHosts{name: "box", wt: &fakeWT{path: "/srv/work/fresh-wt", scratchBase: t.TempDir()}, workspace: "/srv/work"})

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "host", "box", "project", "app")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" || len(fallback.balanced) != 0 || len(fallback.notices) != 0 {
		t.Fatalf("account %q, balanced %v, notices %+v: a session on a remote host is never balanced", sess.Account, fallback.balanced, fallback.notices)
	}
}

func TestRemoteBackendAccountIsNeverMoved(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 10))
	h.SetRemoteBackend(true)

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" || len(fallback.balanced) != 0 || len(fallback.notices) != 0 {
		t.Fatalf("account %q, balanced %v, notices %+v: a remote backend never balances accounts", sess.Account, fallback.balanced, fallback.notices)
	}
}

func TestRefusedTargetKeepsThePlannedAccount(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 10))
	h.validateAccount = func(_, profile string) error {
		if profile == "perso" {
			return errors.New("no login for perso")
		}
		return nil
	}

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "" || len(fallback.notices) != 0 {
		t.Fatalf("account %q, notices %+v: a target failing the account checks is refused and the planned default kept", sess.Account, fallback.notices)
	}
}

func TestLaunchVendorErrorKeepsThePlannedAccount(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 10))
	h.resolveVendor = func(_, _ string) (string, error) {
		return "", errors.New("vendor unresolved")
	}

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude")); err != nil {
		t.Fatalf("session create failed on a launch vendor error: %v", err)
	}

	sess, ok := st.FindSession("fresh")
	if !ok || sess.Account != "" || len(fallback.notices) != 0 {
		t.Fatalf("account %q (found %v), notices %+v: want the planned default kept without a notice", sess.Account, ok, fallback.notices)
	}
}

func TestPreflightNeverBalancesAnAccount(t *testing.T) {
	h, fallback, _ := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 10))

	if err := h.PreflightAccount(args("name", "preview", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	if len(fallback.balanced) != 0 {
		t.Fatalf("preflight balanced accounts %v, want none", fallback.balanced)
	}
}

func TestResumedThenSwitchedSessionKeepsItsAccount(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, spentDefaultWork(), readOf("perso", 10))
	if err := st.AddSession(state.Session{Name: "running", Account: "work", Archived: true, ResumeToken: "resume-me"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionResumeRun(t.Context(), args("name", "running")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.sessionSwitchRun(t.Context(), args("name", "running", "vendor", "claude")); err != nil {
		t.Fatal(err)
	}

	running, ok := st.FindSession("running")
	if !ok || running.Account != "work" {
		t.Fatalf("running session account = %q (found %v), want work untouched by resume and switch", running.Account, ok)
	}
	if len(fallback.balanced) != 0 {
		t.Fatalf("resume and switch balanced accounts %v, want none", fallback.balanced)
	}
}
