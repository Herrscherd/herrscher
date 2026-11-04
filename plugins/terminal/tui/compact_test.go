package tui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func compactModel(t *testing.T, backend *fakeBackend) *model {
	t.Helper()
	backend.sessions = []contracts.SessionInfo{{Name: "api", ChannelID: "c", Vendor: "claude"}}
	m := newModel(backend)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.ensureTab("c")
	m.active = "c"
	return m
}

func TestCompactSendsSessionCompactThroughTheHost(t *testing.T) {
	backend := &fakeBackend{}
	m := compactModel(t, backend)
	cmd := m.runSlash("/compact")
	runCmd(cmd)
	want := []string{"session", "compact", "--name", "api", "--origin", "terminal"}
	if len(backend.dispatched) != 1 || !slices.Equal(backend.dispatched[0], want) {
		t.Fatalf("dispatched %v, want %v", backend.dispatched, want)
	}
}

func TestCompactKeyInOverlaySendsTheSameRequest(t *testing.T) {
	backend := &fakeBackend{}
	m := compactModel(t, backend)
	m.openContext()
	runCmd(m.handleContextKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}))
	if len(backend.dispatched) != 1 || backend.dispatched[0][1] != "compact" {
		t.Fatalf("key c must dispatch session compact, got %v", backend.dispatched)
	}
}

func TestCompactIsRefusedWhileTurnIsBusy(t *testing.T) {
	backend := &fakeBackend{}
	m := compactModel(t, backend)
	m.tabs["c"].busy = true
	runCmd(m.runSlash("/compact"))
	if len(backend.dispatched) != 0 {
		t.Fatalf("a busy tab must not compact, dispatched %v", backend.dispatched)
	}
}

func TestCompactReportsBeforeAndAfterAndGaugeFollows(t *testing.T) {
	backend := &fakeBackend{}
	m := compactModel(t, backend)
	m.tabs["c"].measureContext(contracts.Event{TokensIn: 82_000})
	runCmd(m.runSlash("/compact"))
	m.handleCompactResult(compactResultMsg{channel: "c"})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true, TokensIn: 25_000}})

	if got := m.contextTokens(m.tabs["c"]); got != 25_000 {
		t.Fatalf("gauge source = %d after compaction, want 25000", got)
	}
	transcript := ansi.Strip(strings.Join(entryTexts(m.tabs["c"]), "\n"))
	if !strings.Contains(transcript, "82.0k → 25.0k") {
		t.Fatalf("transcript must report the compaction, got:\n%s", transcript)
	}
}

func TestCompactWithoutUsageReportsUnavailableAfter(t *testing.T) {
	backend := &fakeBackend{}
	m := compactModel(t, backend)
	m.tabs["c"].measureContext(contracts.Event{TokensIn: 82_000})
	runCmd(m.runSlash("/compact"))
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true}})
	transcript := strings.Join(entryTexts(m.tabs["c"]), "\n")
	if !strings.Contains(transcript, "82.0k → n/a") {
		t.Fatalf("a compaction without backend usage must say n/a, got:\n%s", transcript)
	}
}

func TestRefusedCompactionReleasesTheTab(t *testing.T) {
	backend := &fakeBackend{}
	m := compactModel(t, backend)
	runCmd(m.runSlash("/compact"))
	m.handleCompactResult(compactResultMsg{channel: "c", err: errors.New("refused")})
	if m.tabs["c"].busy {
		t.Fatal("a refused compaction must leave the tab idle")
	}
}

func entryTexts(target *tab) []string {
	texts := make([]string, 0, len(target.entries))
	for index := range target.entries {
		texts = append(texts, target.entries[index].text)
	}
	return texts
}
