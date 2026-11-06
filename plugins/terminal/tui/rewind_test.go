package tui

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

type rewindBackend struct {
	fakeBackend

	checkpoints []rewindRow
}

func (r *rewindBackend) Dispatch(args []string) (string, error) {
	r.dispatched = append(r.dispatched, args)
	switch {
	case len(args) >= 2 && args[0] == "session" && args[1] == "checkpoints":
		raw, err := json.Marshal(r.checkpoints)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	case len(args) >= 2 && args[0] == "session" && args[1] == "fork":
		return `{"name":"alpha-fork"}`, nil
	}
	return "ok", nil
}

func sampleCheckpoints() []rewindRow {
	return []rewindRow{
		{Index: 1, At: "14:02", Text: "ajoute une file de messages au composer", Conversation: "start", Files: []rewindFile{{Path: "tui/composer_queue.go", Added: 38}}},
		{Index: 2, At: "14:09", Text: "pense aussi au mode étroit, sous 60 colonnes", Conversation: "u-2", Files: []rewindFile{{Path: "tui/frame.go", Added: 14, Removed: 9}, {Path: "tui/tui.go", Added: 1}}},
		{Index: 3, At: "14:15", Text: "lance les tests et corrige ce qui casse"},
	}
}

func rewindModel(t *testing.T) (*model, *rewindBackend) {
	t.Helper()
	backend := &rewindBackend{checkpoints: sampleCheckpoints()}
	backend.sessions = []contracts.SessionInfo{{Name: "alpha", ChannelID: "a"}}
	m := newModel(backend)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.ensureTab("a")
	m.active = "a"
	return m, backend
}

func pressMsg(m *model, key tea.KeyMsg) tea.Cmd {
	_, cmd := m.Update(key)
	return cmd
}

func typeRune(m *model, r rune) tea.Cmd {
	return pressMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

func openByDoubleEsc(t *testing.T, m *model) {
	t.Helper()
	if cmd := pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("a first idle Esc must not quit")
	}
	if cmd := pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("the second Esc must open the rewind screen, not quit")
	}
	if !m.rwOpen {
		t.Fatal("double Esc on an empty composer must open the rewind screen")
	}
}

func TestDoubleEscOpensRewindWithoutQuitting(t *testing.T) {
	m, backend := rewindModel(t)
	openByDoubleEsc(t, m)
	if m.rwIdx != len(backend.checkpoints)-1 {
		t.Fatalf("selection starts on the most recent message, got %d", m.rwIdx)
	}
}

func TestSingleEscInterruptsATurnInFlight(t *testing.T) {
	m, backend := rewindModel(t)
	m.tabs["a"].busy = true
	if cmd := pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc}); cmd != nil {
		t.Fatal("Esc during a turn must not quit")
	}
	if len(backend.interrupted) != 1 || backend.interrupted[0] != "alpha" {
		t.Fatalf("single Esc must interrupt the turn, interrupted = %v", backend.interrupted)
	}
	if m.rwOpen {
		t.Fatal("Esc during a turn must not open the rewind screen")
	}
}

func TestDoubleEscDuringATurnInterruptsOnceAndOpensNothing(t *testing.T) {
	m, backend := rewindModel(t)
	m.tabs["a"].busy = true
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(backend.interrupted) != 2 || m.rwOpen {
		t.Fatalf("during a turn each Esc interrupts and none opens the rewind: interrupted=%v open=%v", backend.interrupted, m.rwOpen)
	}
}

func TestEscWithTextInComposerDoesNotOpenRewind(t *testing.T) {
	m, _ := rewindModel(t)
	m.input.SetValue("brouillon")
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.rwOpen {
		t.Fatal("Esc must only open the rewind screen on an empty composer")
	}
}

func TestSlowSecondEscDoesNotOpenRewind(t *testing.T) {
	m, _ := rewindModel(t)
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc})
	m.escAt = time.Now().Add(-2 * escDoubleWindow)
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.rwOpen {
		t.Fatal("two Esc further apart than the window must not open the rewind screen")
	}
}

func TestRewindScreenFitsEveryWidth(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m, _ := rewindModel(t)
		m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		openByDoubleEsc(t, m)
		for _, step := range []rewindStep{rewindStepList, rewindStepOptions, rewindStepSure} {
			m.rwStep = step
			for _, line := range strings.Split(m.rewindView(), "\n") {
				if w := lipgloss.Width(line); w > width {
					t.Fatalf("width %d step %d: line is %d wide: %q", width, step, w, line)
				}
			}
		}
	}
}

func TestFilesSitUnderTheMessageBelowEightyColumns(t *testing.T) {
	m, _ := rewindModel(t)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	openByDoubleEsc(t, m)
	view := m.rewindView()
	lines := strings.Split(view, "\n")
	for index, line := range lines {
		if strings.Contains(line, "pense aussi") && index+1 < len(lines) {
			if !strings.Contains(lines[index+1], "+15 −9") {
				t.Fatalf("under 80 columns the file summary must sit on the next line, got %q", lines[index+1])
			}
			return
		}
	}
	t.Fatalf("message line not found in view:\n%s", view)
}

func TestFilesShareTheMessageLineFromEightyColumns(t *testing.T) {
	m, _ := rewindModel(t)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	openByDoubleEsc(t, m)
	for _, line := range strings.Split(m.rewindView(), "\n") {
		if strings.Contains(line, "pense aussi") {
			if !strings.Contains(line, "+15 −9") {
				t.Fatalf("from 80 columns the summary must be on the message line: %q", line)
			}
			return
		}
	}
	t.Fatal("message line not found")
}

func TestUndoKeyRunsUndoOnRewindScreen(t *testing.T) {
	m, backend := rewindModel(t)
	openByDoubleEsc(t, m)
	runCmd(typeRune(m, 'u'))
	if got := backend.dispatched[len(backend.dispatched)-1]; strings.Join(got, " ") != "session undo --name alpha" {
		t.Fatalf("u must undo the last rewind, dispatched %v", got)
	}
	if m.rwOpen {
		t.Fatal("undo must close the rewind screen")
	}
}

func TestConversationRestoreIsRefusedWithReason(t *testing.T) {
	m, backend := rewindModel(t)
	openByDoubleEsc(t, m)
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd := pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("restoring the conversation must not dispatch anything")
	}
	if m.flash != newTranslator().t("tui.rewind.unsupported", nil) {
		t.Fatalf("flash = %q, want the reason", m.flash)
	}
	for _, args := range backend.dispatched {
		if len(args) > 1 && args[1] == "rewind" {
			t.Fatal("no rewind may be sent when the conversation cannot be restored")
		}
	}
}

func TestCodeRestoreNeedsConfirmation(t *testing.T) {
	m, backend := rewindModel(t)
	openByDoubleEsc(t, m)
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyDown})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyDown})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.rwStep != rewindStepSure {
		t.Fatalf("code restore must ask for confirmation first, step = %d", m.rwStep)
	}
	runCmd(typeRune(m, 'y'))
	got := backend.dispatched[len(backend.dispatched)-1]
	if strings.Join(got, " ") != "session rewind --name alpha --to 3 --what code" {
		t.Fatalf("confirmed code restore dispatched %v", got)
	}
}

func TestCodeRestoreCanBeCancelled(t *testing.T) {
	m, backend := rewindModel(t)
	openByDoubleEsc(t, m)
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyDown})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyDown})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	typeRune(m, 'n')
	for _, args := range backend.dispatched {
		if len(args) > 1 && args[1] == "rewind" {
			t.Fatal("a refused confirmation must not restore")
		}
	}
	if m.rwStep != rewindStepOptions {
		t.Fatalf("n must return to the options, step = %d", m.rwStep)
	}
}

func TestForkOpensNewTabForTheForkedSession(t *testing.T) {
	m, backend := rewindModel(t)
	backend.sessions = append(backend.sessions, contracts.SessionInfo{Name: "alpha-fork", ChannelID: "f"})
	openByDoubleEsc(t, m)
	cmd := typeRune(m, 'f')
	msg := runCmd(cmd)
	forked, ok := msg.(forkDoneMsg)
	if !ok {
		t.Fatalf("fork must report back, got %T", msg)
	}
	m.Update(forked)
	if m.active != "f" {
		t.Fatalf("the fork must open its own tab, active = %q", m.active)
	}
}

func TestConversationRestoreAsksForConfirmationWhenTheMessageHasAPosition(t *testing.T) {
	m, backend := rewindModel(t)
	openByDoubleEsc(t, m)
	pressMsg(m, tea.KeyMsg{Type: tea.KeyUp})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyDown})
	pressMsg(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.rwStep != rewindStepSure || m.rwWhat != rewindWhatConversation {
		t.Fatalf("the conversation option must ask first, step = %d what = %q", m.rwStep, m.rwWhat)
	}
	runCmd(typeRune(m, 'y'))
	got := backend.dispatched[len(backend.dispatched)-1]
	if strings.Join(got, " ") != "session rewind --name alpha --to 2 --what conversation" {
		t.Fatalf("confirmed conversation restore dispatched %v", got)
	}
}

func TestRewindSummaryOmitsZeroCounts(t *testing.T) {
	cases := []struct {
		files []rewindFile
		want  string
	}{
		{[]rewindFile{{Path: "a.go", Added: 38}}, "+38 · 1 file"},
		{[]rewindFile{{Path: "a.go", Added: 14, Removed: 9}, {Path: "b.go"}}, "+14 −9 · 2 files"},
		{[]rewindFile{{Path: "a.go", Removed: 4}}, "−4 · 1 file"},
		{nil, "no file"},
	}
	for _, tc := range cases {
		if got := rewindSummary(newTranslator(), tc.files); got != tc.want {
			t.Errorf("rewindSummary(newTranslator(), %v) = %q, want %q", tc.files, got, tc.want)
		}
	}
}
