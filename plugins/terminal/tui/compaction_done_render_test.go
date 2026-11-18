package tui

import (
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func compactionDoneValue(t *testing.T, done eventwire.CompactionDone) string {
	t.Helper()
	value, err := done.Encode()
	if err != nil {
		t.Fatalf("Encode(%+v) = %v", done, err)
	}
	return value
}

func TestRelayedCompactionNoticeRendersTheCodeInTheClientLanguage(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		t.Run(lang.String(), func(t *testing.T) {
			m, tb := failedCompactionTab(t)
			m.localize(lang)
			value := compactionDoneValue(t, eventwire.CompactionDone{Trigger: "manual", Code: uint16(contracts.CodeCompacted)})

			m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionDone, Value: value, Tokens: 90_000})

			want, err := contracts.NewRenderer().Render(lang, contracts.Notice{Code: contracts.CodeCompacted})
			if err != nil || want == "" {
				t.Fatalf("Render = %q, %v, want the notice text", want, err)
			}
			last := tb.entries[len(tb.entries)-1]
			if last.role != roleNotice || last.text != want {
				t.Fatalf("last entry = %+v, want a notice with %q", last, want)
			}
		})
	}
}

func TestRelayedCompactionNoticeDuringANewTurnLeavesTheTurnAlone(t *testing.T) {
	m, tb := failedCompactionTab(t)
	tb.busy = true
	tb.ctxTokens = 120_000
	tb.ctxMeasured = true
	value := compactionDoneValue(t, eventwire.CompactionDone{Trigger: "automatic", Code: uint16(contracts.CodeCompacted)})

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionDone, Value: value, Tokens: 90_000})

	if !tb.busy || tb.ctxTokens != 120_000 || !tb.ctxMeasured {
		t.Fatalf("busy = %t, ctxTokens = %d, measured = %t: a relayed notice must not close the running turn or overwrite its counter", tb.busy, tb.ctxTokens, tb.ctxMeasured)
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleNotice || last.text == "" {
		t.Fatalf("last entry = %+v, want the relayed notice line", last)
	}
}
