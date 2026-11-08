package manager

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

type fakeAccountFallback struct {
	choice  usage.Choice
	planned []string
	notices []contracts.Notice
}

func (f *fakeAccountFallback) Balance(_ context.Context, _, planned string) usage.Choice {
	f.planned = append(f.planned, planned)
	return f.choice
}

func (f *fakeAccountFallback) Notify(_ context.Context, _ state.HomeRef, _ string, _ i18n.Language, notice contracts.Notice) {
	f.notices = append(f.notices, notice)
}

func newFallbackHandler(t *testing.T, choice usage.Choice) (*Handler, *fakeAccountFallback, *state.State) {
	t.Helper()
	h, _, _, _, _, st := newTestHandler(t, "forum")
	st.Home = state.HomeRef{ID: "home", Type: "forum"}
	h.validateAccount = func(_, _ string) error { return nil }
	fallback := &fakeAccountFallback{choice: choice}
	h.SetAccountFallback(fallback)
	return h, fallback, st
}

func renderedNotice(t *testing.T, notice contracts.Notice) string {
	t.Helper()
	text, err := contracts.NewRenderer().Render(i18n.English, notice)
	if err != nil {
		t.Fatalf("render %s: %v", notice.Code.Key(), err)
	}
	return text
}

func TestNewSessionStartsOnTheNextAccountWhenThePlannedOneIsSpent(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, usage.Choice{Outcome: usage.ChoiceSwitched, From: "work", To: "perso"})

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "account", "work")); err != nil {
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
	h, fallback, st := newFallbackHandler(t, usage.Choice{Outcome: usage.ChoiceNoQuota, From: "work"})

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "account", "work")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "work" {
		t.Fatalf("session account = %q, want the planned work kept", sess.Account)
	}
	if len(fallback.notices) != 1 || fallback.notices[0].Code != contracts.CodeAccountNoQuota {
		t.Fatalf("notices = %+v, want one no-quota notice", fallback.notices)
	}
	if text := renderedNotice(t, fallback.notices[0]); !strings.Contains(text, "work") {
		t.Fatalf("rendered notice %q does not name the kept account", text)
	}
}

func TestUnreadableLimitsKeepThePlannedAccountSilently(t *testing.T) {
	h, fallback, st := newFallbackHandler(t, usage.Choice{Outcome: usage.ChoiceKept, From: "work"})

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "account", "work")); err != nil {
		t.Fatal(err)
	}

	sess, _ := st.FindSession("fresh")
	if sess.Account != "work" || len(fallback.notices) != 0 {
		t.Fatalf("account %q, notices %+v: want work kept with no notice", sess.Account, fallback.notices)
	}
}

func TestPreflightNeverBalancesAnAccount(t *testing.T) {
	h, fallback, _ := newFallbackHandler(t, usage.Choice{Outcome: usage.ChoiceSwitched, From: "work", To: "perso"})

	if err := h.PreflightAccount(args("name", "preview", "vendor", "claude", "account", "work")); err != nil {
		t.Fatal(err)
	}

	if len(fallback.planned) != 0 {
		t.Fatalf("preflight balanced accounts %v, want none", fallback.planned)
	}
}

func TestRunningSessionIsNeverMovedByANewOne(t *testing.T) {
	h, _, st := newFallbackHandler(t, usage.Choice{Outcome: usage.ChoiceSwitched, From: "work", To: "perso"})
	if err := st.AddSession(state.Session{Name: "running", Account: "work", ResumeToken: "resume-me"}); err != nil {
		t.Fatal(err)
	}

	if _, err := h.sessionCreateRun(t.Context(), args("name", "fresh", "vendor", "claude", "account", "work")); err != nil {
		t.Fatal(err)
	}

	running, ok := st.FindSession("running")
	if !ok || running.Account != "work" {
		t.Fatalf("running session account = %q (found %v), want work untouched", running.Account, ok)
	}
}
