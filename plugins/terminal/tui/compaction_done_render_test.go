package tui

import (
	"strings"
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
			notice := contracts.Notice{Code: contracts.CodeCompacted}
			value := compactionDoneValue(t, eventwire.CompactionDone{Trigger: "manual", Notice: &notice})

			m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionDone, Value: value, Tokens: 90_000})

			want, err := contracts.NewRenderer().Render(lang, notice)
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
	notice := contracts.Notice{Code: contracts.CodeCompacted}
	value := compactionDoneValue(t, eventwire.CompactionDone{Trigger: "automatic", Notice: &notice})

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionDone, Value: value, Tokens: 90_000})

	if !tb.busy || tb.ctxTokens != 120_000 || !tb.ctxMeasured {
		t.Fatalf("busy = %t, ctxTokens = %d, measured = %t: a relayed notice must not close the running turn or overwrite its counter", tb.busy, tb.ctxTokens, tb.ctxMeasured)
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleNotice || last.text == "" {
		t.Fatalf("last entry = %+v, want the relayed notice line", last)
	}
}

func TestRelayedCompactionFailureDuringANewTurnLeavesTheTurnAlone(t *testing.T) {
	m, tb := failedCompactionTab(t)
	tb.busy = true
	tb.ctxTokens = 120_000
	tb.ctxMeasured = true
	notice := contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Reason", "the bridge closed")}}
	value := compactionFailureValue(t, eventwire.CompactionFailure{Trigger: "automatic", Notice: &notice})

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: value})

	if !tb.busy || tb.ctxTokens != 120_000 || !tb.ctxMeasured {
		t.Fatalf("busy = %t, ctxTokens = %d, measured = %t: a relayed failure must not close the running turn or overwrite its counter", tb.busy, tb.ctxTokens, tb.ctxMeasured)
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleError || last.text == "" {
		t.Fatalf("last entry = %+v, want the relayed failure line", last)
	}
}

func TestRelayedCompactionNoticeKeepsItsArguments(t *testing.T) {
	m, tb := failedCompactionTab(t)
	m.localize(i18n.English)
	notice := contracts.Notice{Code: contracts.CodeCompactUnavailable, Args: []contracts.Arg{contracts.Text("Reason", "the bridge closed")}}
	value := compactionFailureValue(t, eventwire.CompactionFailure{Trigger: "manual", Notice: &notice})

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: value})

	want, err := contracts.NewRenderer().Render(i18n.English, notice)
	if err != nil || !strings.Contains(want, "the bridge closed") {
		t.Fatalf("Render = %q, %v, want the argument in the notice text", want, err)
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleError || last.text != want {
		t.Fatalf("last entry = %+v, want an error with %q", last, want)
	}
}

func TestBareTriggerFromAnOlderHostClosesTheCompaction(t *testing.T) {
	m, tb := failedCompactionTab(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionStarted, Value: "automatic", Tokens: 184_000})

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionDone, Value: "automatic", Text: "résumé", Tokens: 90_000})

	if tb.busy || tb.compaction.active {
		t.Fatal("a bare trigger from an older host must close the compaction like a plain success")
	}
	if tb.ctxTokens != 90_000 {
		t.Fatalf("ctxTokens = %d, want the measured 90000", tb.ctxTokens)
	}
}
