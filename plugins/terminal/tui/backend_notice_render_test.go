package tui

import (
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func TestBackendNoticeRendersInTheClientLanguage(t *testing.T) {
	cases := []struct {
		name string
		lang i18n.Language
	}{
		{"english", i18n.English},
		{"french", i18n.French},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude"}}}
			m := startedModel(f)
			m.localize(tc.lang)
			m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
			tb := m.ensureTab("c")
			m.active = "c"

			m.renderInto(tb, contracts.Event{T: eventwire.EventBackendNotice, Value: strconv.FormatUint(uint64(contracts.CodeResumeLost), 10)})

			want, err := contracts.NewRenderer().Render(tc.lang, contracts.Notice{Code: contracts.CodeResumeLost})
			if err != nil || want == "" {
				t.Fatalf("Render = %q, %v, want the resume notice text", want, err)
			}
			last := tb.entries[len(tb.entries)-1]
			if last.role != roleNotice || last.text != want {
				t.Fatalf("last entry = %+v, want a notice with %q", last, want)
			}
		})
	}
}

func TestBackendNoticeWithANonNumericCodeAddsNoEntry(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude"}}}
	m := startedModel(f)
	tb := m.ensureTab("c")
	m.active = "c"
	before := len(tb.entries)

	m.renderInto(tb, contracts.Event{T: eventwire.EventBackendNotice, Value: "resume_lost"})

	if got := len(tb.entries); got != before {
		t.Fatalf("entries = %d, want %d: a notice whose code is not a number must not be shown", got, before)
	}
}
