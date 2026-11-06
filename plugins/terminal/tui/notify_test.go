package tui

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = saved }()
	run()
	if closeErr := writer.Close(); closeErr != nil {
		t.Fatalf("close writer: %v", closeErr)
	}
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read capture: %v", err)
	}
	return string(out)
}

func notifyModel(t *testing.T, osc9 bool) *model {
	t.Helper()
	m := newModel(&fakeBackend{})
	m.caps.OSC9 = osc9
	m.ensureTab("c")
	m.active = "c"
	return m
}

func TestOSC9IsWrittenWhenWindowLacksFocus(t *testing.T) {
	m := notifyModel(t, true)
	m.Update(tea.BlurMsg{})
	cmd := m.signalCmd("Tour fini")
	out := captureStdout(t, func() { runCmd(cmd) })
	if out != "\x1b]9;Tour fini\x07" {
		t.Fatalf("written %q, want an OSC 9 notification", out)
	}
}

func TestBELIsWrittenWhenTerminalIgnoresOSC9(t *testing.T) {
	m := notifyModel(t, false)
	m.Update(tea.BlurMsg{})
	out := captureStdout(t, func() { runCmd(m.signalCmd("Tour fini")) })
	if out != "\a" {
		t.Fatalf("written %q, want the BEL fallback", out)
	}
}

func TestNothingIsWrittenWhileWindowHasFocus(t *testing.T) {
	m := notifyModel(t, true)
	m.Update(tea.FocusMsg{})
	if cmd := m.signalCmd("Tour fini"); cmd != nil {
		out := captureStdout(t, func() { runCmd(cmd) })
		t.Fatalf("a focused window must not be signalled, wrote %q", out)
	}
}

func TestSignalsAlwaysWhenFocusIsNotReported(t *testing.T) {
	m := notifyModel(t, true)
	out := captureStdout(t, func() { runCmd(m.signalCmd("Tour fini")) })
	if !strings.Contains(out, "\x1b]9;") {
		t.Fatalf("without focus reports the notification must be written, got %q", out)
	}
}

func TestOSC9TextCannotInjectControlSequences(t *testing.T) {
	m := notifyModel(t, true)
	m.Update(tea.BlurMsg{})
	out := captureStdout(t, func() { runCmd(m.signalCmd("a\x07b\x1b[2Jc")) })
	if out != "\x1b]9;ab[2Jc\x07" {
		t.Fatalf("written %q, want the control characters stripped", out)
	}
}

func pressN(m *model) {
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
}

func TestNKeyCyclesWhileToastIsVisible(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	pressN(m)
	if m.notifyMode != notifyTurnEnd {
		t.Fatalf("n with a toast on screen must cycle to turn end, got mode %d", m.notifyMode)
	}
	if m.toast.title == "" {
		t.Fatal("cycling notifications must not dismiss the toast")
	}
}

func TestNKeyCyclesNotificationsInContextOverlay(t *testing.T) {
	m := notifyModel(t, false)
	m.openContext()
	pressN(m)
	if m.notifyMode != notifyTurnEnd {
		t.Fatalf("n in the /context overlay must cycle notifications, got mode %d", m.notifyMode)
	}
}

func TestNKeyIsTypedWithoutToast(t *testing.T) {
	m := notifyModel(t, false)
	pressN(m)
	if m.input.Value() != "n" {
		t.Fatalf("n without a toast must be typed in the composer, got %q", m.input.Value())
	}
	if m.notifyMode != notifyBoth {
		t.Fatal("n without a toast must not change the notification mode")
	}
}

func TestNKeyIsTypedWhenComposerHasText(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	m.input.SetValue("bonjour")
	pressN(m)
	if m.notifyMode != notifyBoth {
		t.Fatal("n must not toggle notifications while the composer holds text")
	}
}

func TestCtrlPJumpsToTheNotifyingSessionWhileToastShows(t *testing.T) {
	m := notifyModel(t, false)
	m.ensureTab("a")
	m.active = "a"
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true}})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.active != "c" {
		t.Fatalf("ctrl+p with a toast must go to the notifying session, active = %q", m.active)
	}
	if m.toast.title != "" {
		t.Fatal("jumping to the session must clear the toast")
	}
}

func TestApprovalToastJumpsToItsSession(t *testing.T) {
	m := notifyModel(t, false)
	m.ensureTab("a")
	m.active = "c"
	m.notifyApproval("a", "run Bash(npm run deploy)")
	if !strings.Contains(strings.Join(m.toast.body, " "), "run Bash") {
		t.Fatalf("the approval toast must name what waits, got %q", m.toast.body)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.active != "a" {
		t.Fatalf("ctrl+p must go to the session asking for approval, active = %q", m.active)
	}
}

func TestCtrlPWithoutToastKeepsItsRole(t *testing.T) {
	m := notifyModel(t, false)
	m.ensureTab("a")
	m.active = "c"
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.active != "c" {
		t.Fatalf("ctrl+p without a toast must not change the tab, active = %q", m.active)
	}
}

func TestTurnEndToastAppearsAndExpires(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true}})
	if !strings.Contains(ansi.Strip(m.View()), "Turn finished") {
		t.Fatal("the turn-end toast must show in the window")
	}
	m.Update(toastExpiredMsg{seq: m.toast.seq})
	if strings.Contains(ansi.Strip(m.View()), "Turn finished") {
		t.Fatal("the toast must disappear when its timer fires")
	}
}

func TestStaleToastTimerKeepsNewerToast(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"a"})
	first := m.toast.seq
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"b"})
	m.Update(toastExpiredMsg{seq: first})
	if m.toast.title == "" {
		t.Fatal("an older timer must not remove a newer toast")
	}
}

func TestToastShowsAgeDeliveryAndJumpHint(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := notifyModel(t, true)
		m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 20})
		m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
		view := ansi.Strip(m.View())
		for _, want := range []string{"0s ago", "OSC 9 sent", "⌃P go there"} {
			if !strings.Contains(view, want) {
				t.Fatalf("width %d: the toast must show %q, window:\n%s", width, want, view)
			}
		}
	}
}

func TestTurnEndToastWithoutStartTimeDoesNotPanic(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "reply", Done: true}})
	if !strings.Contains(ansi.Strip(m.View()), "Turn finished") {
		t.Fatal("a turn without a start time must still raise the toast")
	}
}

func TestToastFitsAnyWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := notifyModel(t, false)
		m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 20})
		m.announce(kindApproval, unicodeGlyphs().attention, "Approbation en attente", "c", []string{strings.Repeat("Bash npm run deploy ", 12)})
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: line of %d cells overflows: %q", width, lipgloss.Width(line), ansi.Strip(line))
			}
		}
	}
}

func TestTerminalsThatHandleOSC9AreProbed(t *testing.T) {
	cases := map[string]bool{"iTerm.app": true, "WezTerm": true, "ghostty": true, "Apple_Terminal": false, "": false}
	for program, want := range cases {
		caps := Probe(func(key string) string {
			if key == "TERM_PROGRAM" {
				return program
			}
			return ""
		})
		if caps.OSC9 != want {
			t.Fatalf("TERM_PROGRAM=%q: OSC9 = %v, want %v", program, caps.OSC9, want)
		}
	}
}

func TestFocusReportsPassThroughTheInputFilter(t *testing.T) {
	focusIn, focusOut := "\x1b[I", "\x1b[O"
	if got := string(filter([]byte(focusIn + focusOut))); got != focusIn+focusOut {
		t.Fatalf("focus reports rewritten to %q, must reach bubbletea unchanged", got)
	}
}

func TestToastAgeAdvancesOnEachTick(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 120, Height: 30})
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	m.toast.at = time.Now().Add(-3 * time.Second)
	if !strings.Contains(ansi.Strip(m.View()), "3s ago") {
		t.Fatal("the toast must show the age of its event")
	}
	m.toast.at = time.Now().Add(-4 * time.Second)
	_, cmd := m.Update(toastTickMsg{seq: m.toast.seq})
	if cmd == nil {
		t.Fatal("a tick while the toast shows must schedule the next tick")
	}
	if !strings.Contains(ansi.Strip(m.View()), "4s ago") {
		t.Fatal("the age must advance when a tick arrives")
	}
}

func TestToastTickStopsWhenTheToastIsGone(t *testing.T) {
	m := notifyModel(t, false)
	m.announce(kindTurnEnd, unicodeGlyphs().success, "Tour fini", "c", []string{"api"})
	seq := m.toast.seq
	m.Update(toastExpiredMsg{seq: seq})
	if _, cmd := m.Update(toastTickMsg{seq: seq}); cmd != nil {
		t.Fatal("a tick for a vanished toast must not be rescheduled")
	}
}

func TestComposerPromptShowsDollarInShellMode(t *testing.T) {
	m := notifyModel(t, false)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.input.SetValue("!ls")
	m.syncComposer()
	m.buildFrame()
	if got := ansi.Strip(m.inputRow()); !strings.HasPrefix(got, "$ !ls") {
		t.Fatalf("shell mode shows the $ prompt, got %q", got)
	}
	m.input.SetValue("ls")
	m.syncComposer()
	m.buildFrame()
	if got := ansi.Strip(m.inputRow()); !strings.HasPrefix(got, "> ls") {
		t.Fatalf("outside shell mode the prompt must be >, got %q", got)
	}
}

func retourFixtureSessions() []contracts.SessionInfo {
	return []contracts.SessionInfo{
		{Name: "tui-v2", ChannelID: "a", Vendor: "claude", Model: "claude-opus-5", Project: "herrscher", Dir: "/home/shan/dev/wt/tui-v2", Gateways: []string{"terminal", "discord"}},
		{Name: "docs-site", ChannelID: "b", Vendor: "codex", Project: "herrscher", Dir: "/home/shan/dev/wt/docs-site", Gateways: []string{"terminal"}},
		{Name: "gobook", ChannelID: "c", Vendor: "cursor", Project: "herrscher", Dir: "/home/shan/dev/wt/gobook", Gateways: []string{"terminal"}},
		{Name: "review", ChannelID: "d", Vendor: "claude", Project: "herrscher", Dir: "/home/shan/dev/wt/review", Gateways: []string{"terminal"}},
	}
}

func retourFixtureOverview(t *testing.T) string {
	t.Helper()
	now := time.Now()
	rows := []SessionOverview{
		{Name: "tui-v2", Vendor: "claude", Gateways: []string{"terminal", "discord"}, State: "working", Cost: 1.42, LastTs: now.Add(-40 * time.Second).UTC().Format(time.RFC3339), LastText: "file de messages"},
		{Name: "docs-site", Vendor: "codex", Gateways: []string{"terminal"}, State: "waiting", Cost: 0.31, LastTs: now.Add(-3 * time.Minute).UTC().Format(time.RFC3339), LastText: "approbation requise"},
		{Name: "gobook", Vendor: "cursor", Gateways: []string{"terminal"}, State: "review", Cost: 0.08, LastTs: now.Add(-9 * time.Minute).UTC().Format(time.RFC3339), LastText: "diff à relire"},
		{Name: "review", Vendor: "claude", Gateways: []string{"terminal"}, State: "done", Cost: 0.12, LastTs: now.Add(-30 * time.Minute).UTC().Format(time.RFC3339), LastText: "terminé"},
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func retourFixtureModel(t *testing.T, width, height int) *model {
	t.Helper()
	fb := &fakeBackend{sessions: retourFixtureSessions(), overview: retourFixtureOverview(t)}
	m := newModel(fb)
	m.Update(tea.WindowSizeMsg{Width: width, Height: height})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "chunk", Text: "Entrée pendant un tour met maintenant le message en file."}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "thinking", Text: "La file vit côté TUI : l’hôte accepte Submit à tout moment."}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "status", Text: "Read tui/tui.go · 1 842 lignes"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "status", Text: "Grep \"tb.busy\" dans tui/ · 14 résultats"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "status", Text: "Edit tui/tui.go +6 −1"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "status", Text: "Bash go test ./plugins/terminal/tui/..."}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "b"}, Event: contracts.Event{T: "chunk", Text: "approbation requise"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "chunk", Text: "diff à relire"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "d"}, Event: contracts.Event{T: "reply", Text: "terminé", Done: true, Cost: 0.12}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "chunk", Text: "", TokensIn: 82000, Tokens: 4200}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "todos", Todos: []contracts.TodoItem{
		{Text: "lire composer et frame", State: "done"},
		{Text: "concevoir la file", State: "done"},
		{Text: "brancher Entrée sur la file", State: "done"},
		{Text: "Échap garde la file intacte", State: "active"},
		{Text: "tests du mode étroit", State: "pending"},
		{Text: "mettre à jour docs/tui.md", State: "pending"},
	}}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "subagent", Subagent: &contracts.Subagent{ID: "1", Name: "explore", Kind: "haiku", State: "done"}}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "subagent", Subagent: &contracts.Subagent{ID: "2", Name: "explore", Kind: "haiku", State: "active"}}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "subagent", Subagent: &contracts.Subagent{ID: "3", Name: "review", Kind: "codex", State: "failed"}}})
	m.syncTabs()
	m.active = "a"
	for _, channel := range []string{"a", "b", "c", "d"} {
		m.tabs[channel].startedAt = time.Now().Add(-2 * time.Minute)
	}
	tab := m.tabs["a"]
	tab.busy = true
	tab.startedAt = time.Now().Add(-14 * time.Second)
	tab.entries = append(tab.entries, entry{role: roleYou, text: "ajoute une file de messages au composer : quand l’agent travaille, ce que je tape attend la fin du tour"})
	m.relayout()
	return m
}

func TestToastKeepsABodyLineOnRetourAtSixtyByTwenty(t *testing.T) {
	m := retourFixtureModel(t, 60, 20)
	m.rwRows = []rewindRow{
		{Index: 1, At: "14:02", Turn: "1", Text: "ajoute une file de messages au composer", Files: []rewindFile{{Path: "a.go", Added: 38}}},
		{Index: 2, At: "14:19", Turn: "2", Text: "relance les tests du composer", Files: []rewindFile{{Path: "b.go", Added: 14, Removed: 9}}},
	}
	m.rwIdx = 1
	m.rwOpt = rewindOptionBoth
	m.rwStep = rewindStepList
	m.rwOpen = true
	m.relayout()
	m.tabs["a"].queue = []queuedInput{{text: "pense aussi au mode étroit, sous 60 colonnes"}, {text: "garde la file après le prochain outil", steer: true}}
	lines := strings.Split(m.toastView(m.toastRows()), "\n")
	if len(lines) < 3 {
		t.Fatalf("the toast must keep its top border, one body line and its bottom border, got %d lines: %q", len(lines), lines)
	}
	if !strings.Contains(lines[1], "review · claude · terminal") {
		t.Errorf("the toast must keep its first body line, got %q", lines[1])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰") {
		t.Errorf("the toast must close with its bottom border, got %q", lines[len(lines)-1])
	}
}

func TestToastStaysVisibleAtTwentyRows(t *testing.T) {
	m, _ := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m.toast = toast{glyph: m.st.glyphs.attention, title: "Approbation attend", signal: "OSC 9 envoyé", at: time.Now()}
	for range 40 {
		m.toast.body = append(m.toast.body, "une ligne du corps du toast qui déborde la place")
	}
	rows := m.toastRows()
	lines := strings.Split(m.toastView(rows), "\n")
	if len(lines) != max(rows, 2) {
		t.Fatalf("the toast must fill the rows it is given: want %d lines, got %d", max(rows, 2), len(lines))
	}
	if !strings.Contains(lines[0], "Approbation attend") {
		t.Errorf("the toast must keep its title border, got %q", lines[0])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰") {
		t.Errorf("the toast must close with its bottom border, got %q", lines[len(lines)-1])
	}
	if rows > 2 && !strings.Contains(lines[len(lines)-2], ellipsis) {
		t.Errorf("the last body row of a squeezed toast must end with %q, got %q", ellipsis, lines[len(lines)-2])
	}
	blank := strings.Repeat(strings.Repeat(" ", 60)+"\n", 19) + strings.Repeat(" ", 60)
	if !strings.Contains(m.overlayToast(blank), "Approbation attend") {
		t.Errorf("the toast must be drawn over the screen at 60x20")
	}
}

func TestToastHidesTheLineBehindItUpToTheEdge(t *testing.T) {
	for _, width := range []int{80, 120, 200} {
		m := notifyModel(t, false)
		m.handleWindowSize(tea.WindowSizeMsg{Width: width, Height: 30})
		behind := strings.Repeat("x", width)
		rendered := strings.Repeat(behind+"\n", 30)
		m.announce(kindApproval, unicodeGlyphs().attention, "Approbation en attente", "c", []string{"Bash ls"})
		toast := m.toastView(m.toastRows())
		column := m.width - lipgloss.Width(firstLine(toast)) - toastColumn
		for _, line := range strings.Split(m.overlayToast(rendered), "\n")[toastRow : toastRow+strings.Count(toast, "\n")+1] {
			if tail := ansi.Strip(line)[column:]; strings.Contains(tail, "x") {
				t.Fatalf("width %d: the line behind the toast shows through on its right: %q", width, ansi.Strip(line))
			}
		}
	}
}
