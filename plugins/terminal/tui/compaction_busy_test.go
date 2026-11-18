package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func compactionTurnTab(t *testing.T) (*model, *tab) {
	t.Helper()
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 92}}}
	m := startedModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"
	return m, tb
}

func TestAutomaticCompactionFailureDuringTurnKeepsBusyUntilDone(t *testing.T) {
	m, tb := compactionTurnTab(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventChunk, Text: "bonjour"})
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionStarted, Value: "automatic", Tokens: 184_000})
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: compactionFailureValue(t, eventwire.CompactionFailure{Trigger: "automatic", Reason: eventwire.CompactReasonBridgeGone})})
	if !tb.busy {
		t.Fatal("a failed automatic compaction during a turn must not release the turn's busy state")
	}
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Done: true})
	if tb.busy {
		t.Fatal("the turn's Done must release the tab")
	}
}

func TestAutomaticCompactionSuccessDuringTurnKeepsBusyUntilDone(t *testing.T) {
	m, tb := compactionTurnTab(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventChunk, Text: "bonjour"})
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionStarted, Value: "automatic", Tokens: 184_000})
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionDone, Value: compactionDoneValue(t, eventwire.CompactionDone{Trigger: "automatic"}), Tokens: 90_000})
	if !tb.busy {
		t.Fatal("a successful automatic compaction during a turn must not release the turn's busy state")
	}
	m.renderInto(tb, contracts.Event{T: eventwire.EventReply, Done: true})
	if tb.busy {
		t.Fatal("the turn's Done must release the tab")
	}
}

func TestIdleAutomaticCompactionFailureReleasesBusy(t *testing.T) {
	m, tb := compactionTurnTab(t)
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionStarted, Value: "automatic", Tokens: 184_000})
	if !tb.busy {
		t.Fatal("a started automatic compaction on an idle tab must mark it busy")
	}
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: compactionFailureValue(t, eventwire.CompactionFailure{Trigger: "automatic", Reason: eventwire.CompactReasonBridgeGone})})
	if tb.busy {
		t.Fatal("a failed automatic compaction that opened busy must release it")
	}
}

func TestManualCompactionFailureKeepsOwnershipAfterItsStartedEvent(t *testing.T) {
	m, tb := compactionTurnTab(t)
	tb.busy = true
	tb.compaction = compaction{active: true, ownsBusy: true}
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionStarted, Value: "manual", Tokens: 90_000})
	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: compactionFailureValue(t, eventwire.CompactionFailure{Trigger: "manual", Reason: eventwire.CompactReasonBridgeGone})})
	if tb.busy {
		t.Fatal("a failed manual compaction must release the busy state it opened")
	}
}

func TestSeededInterruptionRendersInTheTabLanguage(t *testing.T) {
	m, tb := compactionTurnTab(t)
	notice := contracts.Notice{Code: contracts.CodeTurnInterrupted}
	want, err := contracts.NewRenderer().Render(m.tr.lang, notice)
	if err != nil || want == "" {
		t.Fatalf("interruption notice does not render: %q, %v", want, err)
	}
	m.seedScrollback(tb, []contracts.ScrollbackLine{{Role: "notice", Notice: &notice, TurnID: "turn-1"}})
	if len(tb.entries) != 2 || tb.entries[0].role != roleNotice || tb.entries[0].text != want {
		t.Fatalf("entries = %+v, want the interruption rendered as %q then the earlier marker", tb.entries, want)
	}
}

func TestSeededNoticeWithoutRenderingAddsNoLine(t *testing.T) {
	m, tb := compactionTurnTab(t)
	notice := contracts.Notice{Code: contracts.Code(9999)}
	m.seedScrollback(tb, []contracts.ScrollbackLine{{Role: "notice", Notice: &notice, TurnID: "turn-1"}})
	if len(tb.entries) != 1 || tb.entries[0].role != roleScrollback {
		t.Fatalf("entries = %+v, want only the earlier marker, no empty notice line", tb.entries)
	}
}
