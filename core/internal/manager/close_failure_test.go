package manager

import (
	"context"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestARefusedCloseLeavesTheSessionAnswering(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "dirty", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	wt.removeErr = errors.New("uncommitted changes")

	if _, err := h.sessionCloseRun(context.Background(), args("name", "dirty")); err == nil {
		t.Fatal("a dirty worktree must refuse the close")
	}
	if _, ok := st.FindSession("dirty"); !ok {
		t.Fatal("the refused close must keep the session")
	}
	if len(sup.started) != 1 || sup.started[0] != "dirty" {
		t.Fatalf("started=%v, want the bridge put back after the refusal", sup.started)
	}
}

func TestAForcedCloseStillRemovesEverything(t *testing.T) {
	h, _, sup, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "dirty", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	wt.removeErr = errors.New("uncommitted changes")

	if _, err := h.sessionCloseRun(context.Background(), args("name", "dirty", "force", "true")); err != nil {
		t.Fatalf("force close: %v", err)
	}
	if _, ok := st.FindSession("dirty"); ok {
		t.Fatal("the session should be gone")
	}
	if len(sup.started) != 0 {
		t.Fatalf("started=%v, want no bridge restarted for a closed session", sup.started)
	}
}

func TestARefusedCloseNamesTheDirtySessionInEachLanguage(t *testing.T) {
	cases := []struct {
		name  string
		lang  i18n.Language
		wants []string
	}{
		{"english", i18n.English, []string{
			"session dirty has uncommitted changes: commit them, or close it with force:true to discard them",
			"branch session/dirty is kept.",
		}},
		{"french", i18n.French, []string{
			"la session « dirty » a des changements non validés : valide-les, ou ferme-la avec force:true pour les abandonner",
			"la branche session/dirty est conservée.",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cli.Localize(refusedDirtyClose(t), tc.lang).Error()
			for _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Fatalf("rendered %q, want it to contain %q", got, want)
				}
			}
		})
	}
}

func refusedDirtyClose(t *testing.T) error {
	t.Helper()
	h, _, _, wt, _, st := newTestHandler(t, "")
	if err := st.SetHome(state.HomeRef{ID: "cat1", Type: "category"}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSession(state.Session{Name: "dirty", ChannelID: "42", Type: "text", Worktree: "/wt/x"}); err != nil {
		t.Fatal(err)
	}
	wt.removeErr = errors.New("uncommitted changes")
	_, err := h.sessionCloseRun(context.Background(), args("name", "dirty"))
	if err == nil {
		t.Fatal("a dirty worktree must refuse the close")
	}
	noticed, ok := errors.AsType[contracts.NoticeError](err)
	if !ok || noticed.Notice().Code != contracts.CodeSessionCloseDirty {
		t.Fatalf("refusal notice = %v, want %s", err, contracts.CodeSessionCloseDirty.Key())
	}
	return err
}
