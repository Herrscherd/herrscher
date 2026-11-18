package host

import (
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestCLIScrollbackCarriesTheInterruptionText(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		t.Run(lang.String(), func(t *testing.T) {
			partDir := t.TempDir()
			entry := state.TranscriptEntry{Role: "notice", Kind: turnInterruptedKind, TurnID: "t-1"}
			if err := state.AppendTranscript(state.TranscriptPath(partDir, "resumed"), entry); err != nil {
				t.Fatal(err)
			}
			want, err := contracts.NewRenderer().Render(lang, contracts.Notice{Code: contracts.CodeTurnInterrupted})
			if err != nil {
				t.Fatalf("contracts render: %v", err)
			}
			lines := cliScrollback(partDir, "resumed", lang)
			if len(lines) != 1 || lines[0].Role != "notice" || lines[0].Text != want {
				t.Fatalf("cliScrollback = %+v, want one notice line carrying %q", lines, want)
			}
			if !strings.Contains(lines[0].Text, "sans réponse") && lang == i18n.French {
				t.Fatalf("French text = %q, want the French interruption", lines[0].Text)
			}
		})
	}
}
