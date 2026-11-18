package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func compactionFailureValue(t *testing.T, failure eventwire.CompactionFailure) string {
	t.Helper()
	value, err := failure.Encode()
	if err != nil {
		t.Fatalf("Encode(%+v) = %v", failure, err)
	}
	return value
}

func failedCompactionTab(t *testing.T) (*model, *tab) {
	t.Helper()
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude"}}}
	m := startedModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"
	return m, tb
}

func TestCompactionFailureRendersANoticeCodeInTheClientLanguage(t *testing.T) {
	for _, lang := range []i18n.Language{i18n.English, i18n.French} {
		t.Run(lang.String(), func(t *testing.T) {
			m, tb := failedCompactionTab(t)
			m.localize(lang)
			notice := contracts.Notice{Code: contracts.CodeResumeLost}

			m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: compactionFailureValue(t, eventwire.CompactionFailure{Trigger: "manual", Notice: &notice})})

			want, err := contracts.NewRenderer().Render(lang, notice)
			if err != nil || want == "" {
				t.Fatalf("Render = %q, %v, want the notice text", want, err)
			}
			last := tb.entries[len(tb.entries)-1]
			if last.role != roleError || last.text != want {
				t.Fatalf("last entry = %+v, want an error with %q", last, want)
			}
		})
	}
}

func TestCompactionFailureRendersAReasonInTheClientLanguage(t *testing.T) {
	cases := []struct {
		name string
		lang i18n.Language
		want string
	}{
		{"english", i18n.English, "the bridge closed during a compaction"},
		{"french", i18n.French, "le relais s'est fermé pendant une compaction"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, tb := failedCompactionTab(t)
			m.localize(tc.lang)

			m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: compactionFailureValue(t, eventwire.CompactionFailure{Trigger: "manual", Reason: eventwire.CompactReasonBridgeGone})})

			last := tb.entries[len(tb.entries)-1]
			if last.role != roleError || last.text != tc.want {
				t.Fatalf("last entry = %+v, want an error with %q", last, tc.want)
			}
		})
	}
}

func TestCompactionFailureWithAnUnknownCodeOrReasonShowsTheGenericLine(t *testing.T) {
	cases := []struct {
		name    string
		payload eventwire.CompactionFailure
		lang    i18n.Language
		want    string
	}{
		{"unknown code in english", eventwire.CompactionFailure{Trigger: "manual", Notice: &contracts.Notice{Code: 65535}}, i18n.English, "compaction failed"},
		{"unknown code in french", eventwire.CompactionFailure{Trigger: "manual", Notice: &contracts.Notice{Code: 65535}}, i18n.French, "échec de la compaction"},
		{"unknown reason in english", eventwire.CompactionFailure{Trigger: "manual", Reason: "from_a_newer_host"}, i18n.English, "compaction failed"},
		{"unknown reason in french", eventwire.CompactionFailure{Trigger: "manual", Reason: "from_a_newer_host"}, i18n.French, "échec de la compaction"},
		{"unclassified failure", eventwire.CompactionFailure{Trigger: "manual", Reason: eventwire.CompactReasonUnknown}, i18n.English, "compaction failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, tb := failedCompactionTab(t)
			m.localize(tc.lang)

			m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: compactionFailureValue(t, tc.payload)})

			last := tb.entries[len(tb.entries)-1]
			if last.role != roleError || last.text != tc.want {
				t.Fatalf("last entry = %+v, want an error with %q", last, tc.want)
			}
		})
	}
}

func TestCompactionFailureWithAnUnreadablePayloadAddsNoLine(t *testing.T) {
	m, tb := failedCompactionTab(t)
	before := len(tb.entries)

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: "{not json"})

	if got := len(tb.entries); got != before {
		t.Fatalf("entries = %d, want %d: a payload that is not JSON must not be shown", got, before)
	}
}

func TestBareFailureFromAnOlderHostShowsItsText(t *testing.T) {
	m, tb := failedCompactionTab(t)
	m.localize(i18n.English)

	before := len(tb.entries)

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: "automatic", Text: "the bridge closed during a compaction"})

	if len(tb.entries) <= before {
		t.Fatalf("entries = %d, want more than %d: a bare failure must be shown", len(tb.entries), before)
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleError || last.text != "the bridge closed during a compaction" {
		t.Fatalf("last entry = %+v, want the older host's text as an error", last)
	}
}

func TestBareFailureWithoutTextShowsTheGenericLine(t *testing.T) {
	m, tb := failedCompactionTab(t)
	m.localize(i18n.English)

	before := len(tb.entries)

	m.renderInto(tb, contracts.Event{T: eventwire.EventCompactionFailed, Value: "automatic"})

	if len(tb.entries) <= before {
		t.Fatalf("entries = %d, want more than %d: a bare failure must be shown", len(tb.entries), before)
	}
	last := tb.entries[len(tb.entries)-1]
	if last.role != roleError || last.text != "compaction failed" {
		t.Fatalf("last entry = %+v, want the generic failure line", last)
	}
}
