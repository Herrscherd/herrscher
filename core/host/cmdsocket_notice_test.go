package host

import (
	"context"
	"encoding/json"
	"fmt"
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
	serveCommandSocket(ctx, isolatedRuntimeDir(t), sock, dispatcherFunc(func(context.Context, []string) (string, error) {
		return "", serverErr
	}))
	_, handled, err := dispatchLiveCommand(ctx, isolatedRuntimeDir(t), sock, []string{"session", "close", "revue"})
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

func describeNoticeArgs(args []contracts.Arg) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, fmt.Sprintf("%s/%d=%v", a.Name(), a.Kind(), a.Value()))
	}
	return out
}

func TestNotFoundAndUnknownNoticesCarryTheirArgsAndEnglishText(t *testing.T) {
	cases := []struct {
		name     string
		err      *NotFoundError
		text     string
		wantCode contracts.Code
		wantArgs []string
	}{
		{"session", &NotFoundError{Kind: kindSession, Name: "revue"}, `no session named "revue"`, contracts.CodeSessionNotFound, []string{"Name/1=revue"}},
		{"agent", &NotFoundError{Kind: kindAgent, Name: "revue"}, `no agent named "revue"`, contracts.CodeAgentNotFound, []string{"Name/1=revue"}},
		{"schedule", &NotFoundError{Kind: kindSchedule, Name: "nightly"}, `no schedule named "nightly"`, contracts.CodeScheduleNotFound, []string{"Name/1=nightly"}},
		{"generic", &NotFoundError{Kind: kindLead, Name: "x"}, `no lead named "x"`, contracts.CodeEntityNotFound, []string{"Kind/1=lead", "Name/1=x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.text {
				t.Fatalf("Error() = %q, want %q", got, tc.text)
			}
			notice := tc.err.Notice()
			if notice.Code != tc.wantCode {
				t.Fatalf("code = %s, want %s", notice.Code.Key(), tc.wantCode.Key())
			}
			if fmt.Sprint(describeNoticeArgs(notice.Args)) != fmt.Sprint(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", describeNoticeArgs(notice.Args), tc.wantArgs)
			}
		})
	}
}

func TestUnknownNoticesCarryTheirArgsAndEnglishText(t *testing.T) {
	agent := &UnknownError{Kind: kindAgent, Name: "revue"}
	if got, want := agent.Error(), `unknown agent "revue"`; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if notice := agent.Notice(); notice.Code != contracts.CodeUnknownAgent || fmt.Sprint(describeNoticeArgs(notice.Args)) != fmt.Sprint([]string{"Name/1=revue"}) {
		t.Fatalf("agent notice = %s %v", notice.Code.Key(), describeNoticeArgs(notice.Args))
	}
	other := &UnknownError{Kind: kindWorker, Name: "w1"}
	if notice := other.Notice(); notice.Code != contracts.CodeEntityUnknown || fmt.Sprint(describeNoticeArgs(notice.Args)) != fmt.Sprint([]string{"Kind/1=worker", "Name/1=w1"}) {
		t.Fatalf("generic notice = %s %v", notice.Code.Key(), describeNoticeArgs(notice.Args))
	}
}

func TestCommandResponseCarriesTheNoticeUnderItsWireName(t *testing.T) {
	msg := "no session named \"revue\""
	payload, err := json.Marshal(cmdResponse{Err: &msg, Notice: "frame"})
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["notice"] != "frame" {
		t.Fatalf("wire notice field = %v in %s, want key \"notice\"", wire["notice"], payload)
	}
}
