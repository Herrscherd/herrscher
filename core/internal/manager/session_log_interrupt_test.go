package manager

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionLogShowsTheInterruptedTurn(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		t.Run(lang.String(), func(t *testing.T) {
			path := state.TranscriptPath(t.TempDir(), "resumed")
			if err := state.AppendTranscript(path, state.TranscriptEntry{Role: "user", Text: "tour coupé"}); err != nil {
				t.Fatal(err)
			}
			if err := state.AppendTranscript(path, state.TranscriptEntry{Role: "notice", Kind: state.KindTurnInterrupted, TurnID: "t-1"}); err != nil {
				t.Fatal(err)
			}
			want, err := contracts.NewRenderer().Render(lang, contracts.Notice{Code: contracts.CodeTurnInterrupted})
			if err != nil || want == "" {
				t.Fatalf("contracts render = %q, %v; want the interruption text", want, err)
			}
			got := renderShownTranscript(state.ReadTranscript(path, 0), lang)
			if !strings.Contains(got, "notice "+want) {
				t.Fatalf("session log = %q, want the interrupted turn as %q", got, "notice "+want)
			}
		})
	}
}
