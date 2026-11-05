package host

import (
	"context"
	"path/filepath"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/manager"
)

func renderAcrossSocket(t *testing.T, serverErr error, lang i18n.Language) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "n.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveCommandSocket(ctx, sock, dispatcherFunc(func(context.Context, []string) (string, error) {
		return "", serverErr
	}))
	_, handled, err := dispatchLiveCommand(ctx, sock, []string{"session", "close", "revue"})
	if !handled || err == nil {
		t.Fatalf("socket returned handled=%v err=%v, want a failure", handled, err)
	}
	_, rendered := cli.Render(contracts.Result{}, err, lang)
	if rendered == nil {
		t.Fatal("render returned no error text")
	}
	return rendered.Error()
}

func TestSessionNotFoundCrossesTheSocketInTheCallerLanguage(t *testing.T) {
	notFound := &NotFoundError{Kind: kindSession, Name: "revue"}
	if got, want := renderAcrossSocket(t, notFound, i18n.French), "session « revue » introuvable"; got != want {
		t.Fatalf("fr = %q, want %q", got, want)
	}
	if got, want := renderAcrossSocket(t, notFound, i18n.English), "no session named revue"; got != want {
		t.Fatalf("en = %q, want %q", got, want)
	}
}

func TestSessionLimitCrossesTheSocketInTheCallerLanguage(t *testing.T) {
	limit := &manager.SessionLimitError{Max: 3}
	if got, want := renderAcrossSocket(t, limit, i18n.French), "limite de sessions atteinte : 3 sessions autorisées ; ferme-en une avant d'en créer une autre"; got != want {
		t.Fatalf("fr = %q, want %q", got, want)
	}
	if got, want := renderAcrossSocket(t, limit, i18n.English), "session limit reached: 3 sessions allowed; close one before creating another"; got != want {
		t.Fatalf("en = %q, want %q", got, want)
	}
}

func TestNotFoundKindsRouteToTheirDedicatedCodes(t *testing.T) {
	cases := []struct {
		kind string
		want contracts.Code
	}{
		{kindSession, contracts.CodeSessionNotFound},
		{liveSessionKind, contracts.CodeSessionNotFound},
		{kindAgent, contracts.CodeAgentNotFound},
		{kindSchedule, contracts.CodeScheduleNotFound},
		{kindLead, contracts.CodeEntityNotFound},
	}
	for _, tc := range cases {
		if got := (&NotFoundError{Kind: tc.kind, Name: "x"}).Notice().Code; got != tc.want {
			t.Fatalf("kind %q routed to %s, want %s", tc.kind, got.Key(), tc.want.Key())
		}
	}
}
