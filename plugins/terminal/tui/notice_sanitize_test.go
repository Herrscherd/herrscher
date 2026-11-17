package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/usage"
)

func TestNoticeEventEscapesTheControlsOfItsText(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude", AutoCompactPercent: 92}}}
	m := newModel(f)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb := m.ensureTab("c")
	m.active = "c"

	m.renderInto(tb, contracts.Event{T: eventwire.EventNotice, Text: "compte \"work\x1b]0;pwn\a\" retenu"})
	if len(tb.entries) == 0 {
		t.Fatal("no entry rendered for the notice")
	}
	last := tb.entries[len(tb.entries)-1]
	if strings.ContainsRune(last.text, '\x1b') {
		t.Fatalf("notice line carries a raw ESC: %q", last.text)
	}
	if want := `work\x1b]0;pwn\a`; !strings.Contains(last.text, want) {
		t.Errorf("notice line does not show the account as %q: %q", want, last.text)
	}
}

func TestUsageFilterEscapesTheAccountAndVendorItShows(t *testing.T) {
	m := newTestModel()
	m.usage.account = usage.AccountKey{Vendor: "claude", Account: "work\x1b]0;pwn\a"}
	m.usage.vendor = "v\x1b[2J"
	out := m.usageFilters()
	if strings.Contains(out, "\x1b]0;pwn") {
		t.Fatalf("usage filter carries the raw OSC of the account:\n%q", out)
	}
	if want := `work\x1b]0;pwn\a`; !strings.Contains(out, want) {
		t.Errorf("usage filter does not show the account as %q:\n%q", want, out)
	}
	if want := `v\x1b[2J`; !strings.Contains(out, want) {
		t.Errorf("usage filter does not show the vendor as %q:\n%q", want, out)
	}
}
