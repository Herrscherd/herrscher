package tui

import (
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	tea "github.com/charmbracelet/bubbletea"
)

// recorder is an injected launcher: it counts and remembers, and opens nothing.
type recorder struct {
	targets []string
	err     error
}

func (r *recorder) launch(target string) error {
	r.targets = append(r.targets, target)
	return r.err
}

// linkedModel is a model whose transcript holds one agent answer, with both
// launchers replaced by recorders and the first link selected.
func linkedModel(t *testing.T, text string) (*model, *recorder, *recorder) {
	t.Helper()
	m := newTestModel()
	sys, edit := &recorder{}, &recorder{}
	m.sys, m.edit = sys.launch, edit.launch
	tb := m.ensureTab("a")
	m.active = "a"
	tb.appendEntry(entry{role: roleAgent, text: text})
	m.renderTranscript(tb, 200) // finds the links the way a real frame does
	if len(m.links) == 0 {
		t.Fatalf("the fixture must contain a link: %q", text)
	}
	m.linkIdx = 0
	return m, sys, edit
}

func TestOpenKeyOpensTheSelectedLink(t *testing.T) {
	m, sys, edit := linkedModel(t, "see https://example.com/a")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if len(sys.targets) != 1 {
		t.Fatalf("the open key must call the system launcher once, got %v", sys.targets)
	}
	if sys.targets[0] != "https://example.com/a" {
		t.Fatalf("the launcher must get the resolved target, got %q", sys.targets[0])
	}
	if len(edit.targets) != 0 {
		t.Fatalf("a URL must not reach the editor: %v", edit.targets)
	}
}

// This is the load-bearing assertion of the whole feature. A link opens because
// an operator asked for it, on their own machine, and for no other reason. An
// agent that can make the terminal open a URL by saying one has a way out of the
// terminal — so a message arriving and a turn completing must both leave the
// counter at zero, whatever the transcript contains.
func TestNothingOpensWithoutAGesture(t *testing.T) {
	m, sys, edit := linkedModel(t, "already here: https://example.com/a and core/host/turnloop.go:42")

	conv := contracts.Conversation{ID: "a"}
	events := []contracts.Event{
		{T: "chunk", Text: "go to https://evil.test/x now"},
		{T: "status", Text: "webfetch https://evil.test/y"},
		{T: "thinking", Text: "maybe https://evil.test/z"},
		{T: "reply", Text: "done: https://evil.test/w and main.go:1", Done: true, Cost: 0.01},
		{T: "reset"},
		{T: "abandoned"},
	}
	for _, e := range events {
		m.Update(eventMsg(RoutedEvent{Conv: conv, Event: e}))
		// Every frame the events produced is rendered too: finding a link must not
		// be the same thing as following it.
		m.renderTranscript(m.tabs["a"], 200)
	}
	// And the animation timer, which repaints on its own.
	m.Update(spinMsg{})

	if len(sys.targets) != 0 || len(edit.targets) != 0 {
		t.Fatalf("nothing may open without an operator gesture; system=%v editor=%v", sys.targets, edit.targets)
	}
}

// A file reference goes to the editor, at its line. Handing it to xdg-open would
// launch whatever the desktop thinks a .go file is, which is not the point.
func TestFileReferenceGoesToTheEditorAtItsLine(t *testing.T) {
	m, sys, edit := linkedModel(t, "broke at core/host/turnloop.go:42")
	if m.links[0].Kind != LinkFile {
		t.Fatalf("the fixture must be a file reference: %+v", m.links[0])
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if len(edit.targets) != 1 || edit.targets[0] != "core/host/turnloop.go:42" {
		t.Fatalf("the editor must get the reference with its line: %v", edit.targets)
	}
	if len(sys.targets) != 0 {
		t.Fatalf("a file must not reach the system handler: %v", sys.targets)
	}
}

// A launcher that fails reports once and the session carries on. Losing the turn
// because a desktop helper is missing would be a poor trade.
func TestLauncherErrorSurfacesAndTheTurnSurvives(t *testing.T) {
	m, sys, _ := linkedModel(t, "see https://example.com/a")
	sys.err = errors.New("xdg-open: not found")
	before := len(m.tabs["a"].entries)

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if !strings.Contains(m.flash, "not found") {
		t.Fatalf("the failure must reach the status line: %q", m.flash)
	}
	if got := len(m.tabs["a"].entries); got != before {
		t.Fatalf("the transcript must be untouched: %d entries, was %d", got, before)
	}
}

// A markdown label can name one destination and point at another, so the status
// line shows the resolved target. This is display, not a confirmation: the
// operator sees where the gesture goes before making it.
func TestStatusLineShowsTheTargetNotTheLabel(t *testing.T) {
	m := newTestModel()
	// Set straight on the model: glamour resolves a markdown link before the
	// transcript is ever scanned, so the lying label only reaches findLinks from a
	// still-streaming block. The status line's contract is the same either way.
	m.links = []Link{{Label: "example.com", Target: "https://phish.test/steal", Kind: LinkURL}}
	m.linkIdx = 0
	out := m.footer()
	if !strings.Contains(out, "https://phish.test/steal") {
		t.Fatalf("the status line must resolve the target: %q", out)
	}
	if strings.Contains(out, "example.com]") || strings.Contains(out, "[example.com") {
		t.Fatalf("the status line must not repeat the label as the destination: %q", out)
	}
}

// The selection is reachable from the keyboard and wraps back to nothing, so an
// operator can always put the gesture down again.
func TestLinkSelectionCyclesAndReleases(t *testing.T) {
	m, _, _ := linkedModel(t, "https://example.com/a then https://example.com/b")
	m.linkIdx = -1
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.linkIdx != 0 {
		t.Fatalf("the first press must select the first link, got %d", m.linkIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.linkIdx != 1 {
		t.Fatalf("the second press must advance, got %d", m.linkIdx)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if m.linkIdx != -1 {
		t.Fatalf("past the last link the selection must be released, got %d", m.linkIdx)
	}
}

// The editor launcher splits the reference the way every editor since vi has
// taken it, and says so plainly when there is no editor to run.
func TestEditorLauncherNeedsAnEditor(t *testing.T) {
	err := editorLauncher(func(string) string { return "" })("main.go:12")
	if err == nil || !strings.Contains(err.Error(), "EDITOR") {
		t.Fatalf("no $EDITOR must be reported as such: %v", err)
	}
}

func TestEditorTargetSplitsFileAndLine(t *testing.T) {
	file, line := splitFileRef("core/host/turnloop.go:42")
	if file != "core/host/turnloop.go" || line != "42" {
		t.Fatalf("splitFileRef = %q, %q", file, line)
	}
	if file, line := splitFileRef("Makefile"); file != "Makefile" || line != "" {
		t.Fatalf("a reference with no line must keep the whole path: %q, %q", file, line)
	}
}
