package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type refusingBackend struct {
	fakeBackend
}

const modeRefusal = "host refused the mode"

func (r *refusingBackend) Dispatch(args []string) (string, error) {
	r.dispatched = append(r.dispatched, args)
	return "", errors.New(modeRefusal)
}

func modeSession(f Backend) *model {
	m := newModel(f)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.ensureTab("a")
	m.active = "a"
	return m
}

func namedSession() []contracts.SessionInfo {
	return []contracts.SessionInfo{{Name: "s", ChannelID: "a"}}
}

func pressKey(m *model, key tea.KeyMsg) {
	cmd, _ := m.handleKey(key)
	if msg := runCmd(cmd); msg != nil {
		m.Update(msg)
	}
}

func pressShiftTab(m *model) {
	pressKey(m, tea.KeyMsg{Type: tea.KeyShiftTab})
}

func lastDispatch(t *testing.T, f *fakeBackend) []string {
	t.Helper()
	if len(f.dispatched) == 0 {
		t.Fatal("expected a host dispatch, got none")
	}
	return f.dispatched[len(f.dispatched)-1]
}

func TestShiftTabCyclesFourModesAndTellsTheHost(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)

	want := []struct {
		mode modeID
		host string
	}{
		{modeEdits, "accept-edits"},
		{modePlan, "plan"},
		{modeAuto, "auto"},
		{modeManual, "ask"},
	}
	for _, step := range want {
		pressShiftTab(m)
		if got := m.tabs["a"].modeID(); got != step.mode {
			t.Fatalf("after shift+tab the mode is %q, want %q", got, step.mode)
		}
		args := lastDispatch(t, f)
		wantArgs := []string{"session", "set-mode", "--name", "s", "--mode", step.host}
		if strings.Join(args, " ") != strings.Join(wantArgs, " ") {
			t.Fatalf("dispatch = %v, want %v", args, wantArgs)
		}
	}
}

func TestShiftTabLeavesTheActiveTabAlone(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.ensureTab("b")
	m.active = "a"
	pressShiftTab(m)
	if m.active != "a" {
		t.Fatalf("shift+tab must not switch tabs, active is %q", m.active)
	}
}

func TestRefusedModeSwitchRevertsTheMode(t *testing.T) {
	f := &refusingBackend{fakeBackend: fakeBackend{sessions: namedSession()}}
	m := modeSession(f)
	pressShiftTab(m)
	if got := m.tabs["a"].modeID(); got != modeManual {
		t.Fatalf("refused switch must keep the previous mode, got %q", got)
	}
	if !strings.Contains(tabText(m.tabs["a"]), modeRefusal) {
		t.Fatalf("refusal must be shown in the tab: %q", tabText(m.tabs["a"]))
	}
}

func TestModeSlashSetsAModeOrCycles(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.input.SetValue("/mode plan")
	runCmd(m.handleEnter())
	if got := m.tabs["a"].modeID(); got != modePlan {
		t.Fatalf("/mode plan must set plan, got %q", got)
	}
	if got := lastDispatch(t, f); got[len(got)-1] != "plan" {
		t.Fatalf("/mode plan must send plan to the host: %v", got)
	}
}

func TestPlanReplyOpensTheValidationMenuOnlyInPlanMode(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	reply := RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "1. read the code", Done: true}}

	m.route(reply)
	if m.planMenu != nil {
		t.Fatal("a reply outside plan mode must not open the plan menu")
	}

	m.tabs["a"].setMode(modePlan)
	m.route(reply)
	if m.planMenu == nil {
		t.Fatal("a plan reply must open the plan validation menu")
	}
}

func TestPlanMenuFirstChoiceSwitchesToAcceptEditsThenSubmits(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.tabs["a"].setMode(modePlan)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "1. read", Done: true}})

	cmd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	settle(m, cmd)

	if m.planMenu != nil {
		t.Fatal("choosing an option must close the plan menu")
	}
	if got := lastDispatch(t, f); got[len(got)-1] != "accept-edits" {
		t.Fatalf("validating in accept edits must send accept-edits: %v", got)
	}
	if len(f.submitted) != 1 || f.submitted[0].text != planApproved {
		t.Fatalf("validation must submit %q, got %+v", planApproved, f.submitted)
	}
}

func TestPlanMenuSecondChoiceValidatesInManual(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.tabs["a"].setMode(modePlan)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "1. read", Done: true}})

	pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	pressKey(m, tea.KeyMsg{Type: tea.KeyEnter})

	if got := lastDispatch(t, f); got[len(got)-1] != "ask" {
		t.Fatalf("validating in manual must send ask: %v", got)
	}
	if got := m.tabs["a"].modeID(); got != modeManual {
		t.Fatalf("mode after manual validation = %q", got)
	}
}

func TestPlanMenuEscKeepsPlanningWithoutTouchingTheHost(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.tabs["a"].setMode(modePlan)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "1. read", Done: true}})
	before := len(f.dispatched)

	pressKey(m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.planMenu != nil {
		t.Fatal("esc must close the plan menu")
	}
	if len(f.dispatched) != before || len(f.submitted) != 0 {
		t.Fatal("keeping the plan open must neither change the mode nor submit")
	}
	if !strings.Contains(tabText(m.tabs["a"]), "reste ouvert") {
		t.Fatalf("keeping the plan open must say so in the tab: %q", tabText(m.tabs["a"]))
	}
	if got := m.tabs["a"].modeID(); got != modePlan {
		t.Fatalf("mode must stay plan, got %q", got)
	}
}

func TestModeStripAndStatusFitEveryWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		f := &fakeBackend{sessions: namedSession()}
		m := modeSession(f)
		m.tabs["a"].setMode(modePlan)
		checkModeStripFits(t, m, width)
	}
}

func checkModeStripFits(t *testing.T, m *model, width int) {
	t.Helper()
	strip := m.modeStripView(m.tabs["a"], width)
	if len(strip) != 2 {
		t.Fatalf("width %d: strip must be two lines, got %d", width, len(strip))
	}
	for _, line := range strip {
		if w := lipgloss.Width(line); w > width {
			t.Fatalf("width %d: strip line is %d columns: %q", width, w, ansi.Strip(line))
		}
	}
	plain := ansi.Strip(strip[0])
	if !strings.Contains(plain, "Plan") {
		t.Fatalf("width %d: strip must name the current mode: %q", width, plain)
	}
	status := ansi.Strip(m.modeChip(m.tabs["a"]))
	if !strings.HasPrefix(status, "Plan") {
		t.Fatalf("width %d: the mode chip must come first in the status bar: %q", width, status)
	}
}

func TestPlanMenuFitsNarrowWidths(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		f := &fakeBackend{sessions: namedSession()}
		m := modeSession(f)
		m.width = width
		m.openPlanMenu("a")
		for _, line := range strings.Split(m.planMenuView(), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Fatalf("width %d: plan menu line is %d columns: %q", width, w, ansi.Strip(line))
			}
		}
	}
}

type replyBackend struct {
	fakeBackend

	reply string
}

func (r *replyBackend) Dispatch(args []string) (string, error) {
	r.dispatched = append(r.dispatched, args)
	return r.reply, nil
}

func TestTabStartsFromTheSessionStoredMode(t *testing.T) {
	cases := map[string]modeID{"plan": modePlan, "accept-edits": modeEdits, "bypass": modeAuto, "strict": modeManual}
	for host, want := range cases {
		f := &replyBackend{fakeBackend: fakeBackend{sessions: namedSession()}, reply: host}
		m := newModel(f)
		m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		m.ensureTab("a")
		m.active = "a"

		msg := runCmd(m.getModeCmd("a", "s"))
		m.Update(msg)
		if got := m.tabs["a"].modeID(); got != want {
			t.Fatalf("stored %q: tab mode %q, want %q", host, got, want)
		}
	}
}

func TestUnknownStoredReplyLeavesTheTabAlone(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.Update(modeLoadedMsg{channel: "a", host: "ok"})
	if m.tabs["a"].mode != modeNone {
		t.Fatalf("an unrecognised reply must not set the mode, got %s", m.tabs["a"].mode)
	}
}

func TestClickOnAStripSegmentSwitchesMode(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	target := m.tabs["a"]
	hits := m.modeStripHits()
	strip := m.buildFrame()
	if !strip.stripDown {
		t.Fatal("the mode strip must be part of the frame")
	}

	_, cmd := m.Update(tea.MouseMsg{X: hits[2].start, Y: strip.stripRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(runCmd(cmd))
	if got := target.modeID(); got != modePlan {
		t.Fatalf("clicking the Plan segment left the mode at %q", got)
	}
	if got := lastDispatch(t, f); got[len(got)-1] != "plan" {
		t.Fatalf("clicking Plan must send plan to the host: %v", got)
	}
}

func TestClickOutsideTheStripSegmentsChangesNothing(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	strip := m.buildFrame()

	_, cmd := m.Update(tea.MouseMsg{X: 0, Y: strip.stripRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(runCmd(cmd))
	_, cmd = m.Update(tea.MouseMsg{X: 5, Y: strip.stripRow - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m.Update(runCmd(cmd))
	if len(f.dispatched) != 0 {
		t.Fatalf("clicks outside the segments must not reach the host: %v", f.dispatched)
	}
}

func TestModeStripKeepsOneGapAroundEverySegment(t *testing.T) {
	m, tb := barModelAt(t, "")
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	tb.setMode(modeManual)
	strip := ansi.Strip(m.modeStripView(tb, 120)[0])
	if !strings.Contains(strip, "Manuel  →  Édits acceptés  →  Plan  →  ⚠ Auto") {
		t.Errorf("mode strip = %q, want an even gap around each segment", strip)
	}
}

func TestPlanMenuShiftTabLeavesTheMenuForTheNextMode(t *testing.T) {
	f := &fakeBackend{sessions: namedSession()}
	m := modeSession(f)
	m.tabs["a"].setMode(modePlan)
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "1. read", Done: true}})
	if !strings.Contains(ansi.Strip(m.planMenuView()), "⇧⇥ changer de mode") {
		t.Fatalf("the plan menu footer must offer ⇧⇥: %q", ansi.Strip(m.planMenuView()))
	}

	cmd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	settle(m, cmd)

	if m.planMenu != nil {
		t.Fatal("⇧⇥ must close the plan menu")
	}
	if got := m.tabs["a"].modeID(); got != nextModeID(modePlan) {
		t.Fatalf("⇧⇥ from the plan menu must move to %s, got %s", nextModeID(modePlan), got)
	}
}

func TestModeLabelsStayWholeInShortWindows(t *testing.T) {
	for _, size := range []struct{ width, height int }{{60, 20}, {80, 30}, {120, 30}} {
		m, _ := barModelAt(t, "")
		m.Update(tea.WindowSizeMsg{Width: size.width, Height: size.height})
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "Manuel  →  Édits acceptés  →  Plan") && size.width >= 60 {
			t.Fatalf("%dx%d: the mode strip must keep whole labels:\n%s", size.width, size.height, view)
		}
		if strings.Contains(view, "Man  →") || strings.Contains(view, "Édits  →") {
			t.Fatalf("%dx%d: a mode label must not be shortened:\n%s", size.width, size.height, view)
		}
	}
}
