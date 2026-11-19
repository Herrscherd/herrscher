package manager

import (
	"context"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionLogShowsTheInterruptedTurn(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		t.Run(lang.String(), func(t *testing.T) {
			got := sessionLogOfInterruptedTurn(t, lang)
			want := interruptionTextIn(t, lang)
			if !strings.Contains(got, "notice "+want) {
				t.Fatalf("session log = %q, want the interrupted turn as %q", got, "notice "+want)
			}
		})
	}
}

func sessionLogOfInterruptedTurn(t *testing.T, lang i18n.Language) string {
	t.Helper()
	partDir := t.TempDir()
	path := state.TranscriptPath(partDir, "resumed")
	if err := state.AppendTranscript(path, state.TranscriptEntry{Role: "user", Text: "tour coupé"}); err != nil {
		t.Fatal(err)
	}
	if err := state.AppendTranscript(path, state.TranscriptEntry{Role: "notice", Kind: state.KindTurnInterrupted, TurnID: "t-1"}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{messages: clitext.NewMessages(), partDir: partDir, lang: i18n.English}
	ctx := clitext.WithCallerLanguage(context.Background(), lang.String())
	res, err := h.sessionLogRun(ctx, contracts.Input{Args: map[string]string{"name": "resumed"}})
	if err != nil {
		t.Fatalf("session log: %v", err)
	}
	got, ok := res.Data().(string)
	if !ok {
		t.Fatalf("session log data = %T, want a string", res.Data())
	}
	return got
}

func interruptionTextIn(t *testing.T, lang i18n.Language) string {
	t.Helper()
	want, err := contracts.NewRenderer().Render(lang, contracts.Notice{Code: contracts.CodeTurnInterrupted})
	if err != nil || want == "" {
		t.Fatalf("contracts render = %q, %v; want the interruption text", want, err)
	}
	return want
}
