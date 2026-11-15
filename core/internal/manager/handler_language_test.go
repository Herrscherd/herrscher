package manager

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionVerbsRenderInTheOperatorLanguage(t *testing.T) {
	h, _, _, _, _, st := newTestHandler(t, "")
	h.SetLanguage(i18n.French)
	if out, err := cli.Plain(h.sessionListRun(context.Background(), args())); err != nil || out != "Aucune session active." {
		t.Fatalf("empty list in French: out=%q err=%v", out, err)
	}
	if err := st.AddSession(state.Session{Name: "demo", ChannelID: "c1", Type: "text"}); err != nil {
		t.Fatal(err)
	}
	if out, err := cli.Plain(h.sessionListRun(context.Background(), args())); err != nil || !strings.HasPrefix(out, "Sessions actives :\n") {
		t.Fatalf("list header in French: out=%q err=%v", out, err)
	}
	if out, err := cli.Plain(h.sessionWhoRun(context.Background(), args("name", "demo"))); err != nil || out != "Personne n'a encore écrit dans cette session." {
		t.Fatalf("who in French: out=%q err=%v", out, err)
	}
	if out, err := cli.Plain(h.sessionRenameRun(context.Background(), args("name", "demo", "to", "neuf"))); err != nil || out != "✎ Session **demo** renommée en **neuf**." {
		t.Fatalf("rename in French: out=%q err=%v", out, err)
	}
}

func TestSetLanguageFallsBackToEnglishWhenUndefined(t *testing.T) {
	h, _, _, _, _, _ := newTestHandler(t, "")
	h.SetLanguage(i18n.Language(0))
	if out, err := cli.Plain(h.sessionListRun(context.Background(), args())); err != nil || out != "No active sessions." {
		t.Fatalf("undefined language: out=%q err=%v", out, err)
	}
}
