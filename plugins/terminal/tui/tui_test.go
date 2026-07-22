package tui

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	tea "github.com/charmbracelet/bubbletea"
)

func newTestModel() *model { return newModel(nil) }

// runCmd executes a tea.Cmd (and any single follow-up message) so tests can
// drive commands that now run off the Update loop. Nil is a no-op.
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestRoutedEventLandsInOwnTab(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "chunk", Text: "hello-a"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "b"}, Event: contracts.Event{T: "chunk", Text: "hello-b"}})

	if got := tabText(m.tabs["a"]); !strings.Contains(got, "hello-a") {
		t.Fatalf("tab a missing its line: %q", got)
	}
	if got := tabText(m.tabs["b"]); strings.Contains(got, "hello-a") {
		t.Fatalf("tab b leaked tab a's line: %q", got)
	}
}

func TestUnreadSetOnInactiveTab(t *testing.T) {
	m := newTestModel()
	m.ensureTab("a") // first tab becomes active
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "b"}, Event: contracts.Event{T: "chunk", Text: "x"}})
	if !m.tabs["b"].unread {
		t.Fatal("event on inactive tab b must mark it unread")
	}
	if m.tabs["a"].unread {
		t.Fatal("active tab a must not be unread")
	}
}

func TestSwitchTabClearsUnread(t *testing.T) {
	m := newTestModel()
	m.ensureTab("a")
	m.ensureTab("b")
	m.tabs["b"].unread = true
	m.active = "a"
	m.switchTab(1) // move to next tab -> b
	if m.active != "b" {
		t.Fatalf("active = %q, want b", m.active)
	}
	if m.tabs["b"].unread {
		t.Fatal("switching to b must clear its unread")
	}
}

func TestRenderEventShowsCostPerTab(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "done", Done: true, Cost: 0.0042}})
	joined := tabText(m.tabs["a"])
	if !strings.Contains(joined, "done") || !strings.Contains(joined, "$0.0042") {
		t.Fatalf("cost/reply dropped: %q", joined)
	}
}

func TestRenderEventOmitsZeroCost(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "done", Done: true}})
	for _, e := range m.tabs["a"].entries {
		if strings.Contains(e.text, "$") {
			t.Fatalf("zero-cost turn must not show a cost line; got %q", e.text)
		}
	}
}

func TestRenderEventMarksAbandonedPerTab(t *testing.T) {
	m := newTestModel()
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "abandoned"}})
	if !strings.Contains(tabText(m.tabs["a"]), "abandoned") {
		t.Fatal("abandoned not surfaced")
	}
}

// fakeBackend satisfies the Backend interface for unit tests.
type closedSession struct {
	name  string
	force bool
}

type fakeBackend struct {
	dispatched  [][]string
	closed      []closedSession
	sessions    []contracts.SessionInfo
	fe          chan RoutedEvent
	scrollback  map[string][]contracts.ScrollbackLine
	resumed     []string
	submitted   []submittedTurn
	interrupted []string
}

// submittedTurn records a Submit call so tests can assert attachments were wired.
type submittedTurn struct {
	channel string
	text    string
	atts    []Attachment
}

func (f *fakeBackend) Frontend() <-chan RoutedEvent { return f.fe }
func (f *fakeBackend) Submit(channel, text string, atts []Attachment) {
	f.submitted = append(f.submitted, submittedTurn{channel: channel, text: text, atts: atts})
}
func (f *fakeBackend) Sessions() []contracts.SessionInfo { return f.sessions }
func (f *fakeBackend) Dispatch(args []string) (string, error) {
	f.dispatched = append(f.dispatched, args)
	return "ok", nil
}
func (f *fakeBackend) Close(name string, force bool) (string, error) {
	f.closed = append(f.closed, closedSession{name: name, force: force})
	return "ok", nil
}
func (f *fakeBackend) Scrollback(name string) []contracts.ScrollbackLine {
	return f.scrollback[name]
}
func (f *fakeBackend) Resume(name string) (string, error) {
	f.resumed = append(f.resumed, name)
	return "resumed " + name, nil
}
func (f *fakeBackend) Interrupt(name string) bool {
	f.interrupted = append(f.interrupted, name)
	return true
}
func (f *fakeBackend) Commands() []CommandSpec {
	return []CommandSpec{
		{Name: "session create", Args: "--name <name>", Desc: "start a session"},
		{Name: "session list", Desc: "list sessions"},
		{Name: "agent create", Args: "--name <name>", Desc: "add an agent"},
	}
}

func TestSlashLineDispatches(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.input.SetValue("/session list")
	runCmd(m.handleEnter())
	if len(f.dispatched) != 1 || f.dispatched[0][0] != "session" {
		t.Fatalf("slash line not dispatched: %+v", f.dispatched)
	}
}

func TestBareSlashDoesNotDispatch(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.active = "a"
	// A lone "/" is the palette prefix; pressing Enter on it must not dispatch an
	// empty argv (which the hub rejects with `error: command ""` on startup).
	m.input.SetValue("/")
	runCmd(m.handleEnter())
	if len(f.dispatched) != 0 {
		t.Fatalf("bare / must not dispatch a command: %+v", f.dispatched)
	}
}

func TestPlainLineSubmits(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.input.SetValue("hello world")
	runCmd(m.handleEnter())
	if len(f.dispatched) != 0 {
		t.Fatalf("plain line must not dispatch: %+v", f.dispatched)
	}
}

func TestReopenedTabSeedsDimmedScrollback(t *testing.T) {
	f := &fakeBackend{
		sessions: []contracts.SessionInfo{{Name: "s", ChannelID: "c"}},
		scrollback: map[string][]contracts.ScrollbackLine{
			"s": {{Role: "user", Text: "old-q"}, {Role: "assistant", Text: "old-a"}},
		},
	}
	m := newModel(f)
	// Opening the tab (a live event on a not-yet-seen channel) must seed the
	// recorded history first, then append the live line after it.
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "c"}, Event: contracts.Event{T: "chunk", Text: "live"}})
	joined := tabText(m.tabs["c"])
	if !strings.Contains(joined, "old-q") || !strings.Contains(joined, "old-a") {
		t.Fatalf("scrollback not seeded: %q", joined)
	}
	if qi, li := strings.Index(joined, "old-q"), strings.Index(joined, "live"); qi < 0 || li < 0 || qi > li {
		t.Fatalf("scrollback must precede live output: %q", joined)
	}
}

func TestSyncTabsSkipsArchived(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "live", ChannelID: "c1"},
		{Name: "arch", ChannelID: "c2", Archived: true},
	}}
	m := newModel(f)
	m.syncTabs()
	if _, ok := m.tabs["c1"]; !ok {
		t.Fatal("live session must get a tab")
	}
	if _, ok := m.tabs["c2"]; ok {
		t.Fatal("archived session must not auto-open a tab")
	}
}

func TestSlashResumeOpensPicker(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "a", ChannelID: "c", Archived: true, Resumable: true}}}
	m := newModel(f)
	m.input.SetValue("/resume")
	runCmd(m.handleEnter())
	if !m.resumeOpen {
		t.Fatal("/resume must open the picker")
	}
	if len(f.dispatched) != 0 {
		t.Fatalf("/resume must not dispatch to backend: %+v", f.dispatched)
	}
}

func TestResumePickerRevivesArchivedAndFocusesLive(t *testing.T) {
	f := &fakeBackend{
		sessions: []contracts.SessionInfo{
			{Name: "live", ChannelID: "cl", LastTs: "2026-07-20T10:00:00Z"},
			{Name: "arch", ChannelID: "ca", Archived: true, Resumable: true, LastTs: "2026-07-20T12:00:00Z"},
		},
		scrollback: map[string][]contracts.ScrollbackLine{
			"arch": {{Role: "user", Text: "past"}},
		},
	}
	m := newModel(f)

	m.openResume()
	if !m.resumeOpen {
		t.Fatal("picker should be open")
	}
	// newest LastTs first → the archived row sorts to the top.
	if len(m.resumeRows) != 2 || m.resumeRows[0].Name != "arch" {
		t.Fatalf("rows not sorted by LastTs desc: %+v", m.resumeRows)
	}

	// Enter on the archived row revives it and opens+focuses its (seeded) tab.
	cmd := m.chooseResume()
	if m.active != "ca" {
		t.Fatalf("archived choice must focus its tab; active=%q", m.active)
	}
	if _, ok := m.tabs["ca"]; !ok {
		t.Fatal("archived choice must open a tab")
	}
	if got := tabText(m.tabs["ca"]); !strings.Contains(got, "past") {
		t.Fatalf("reopened tab not seeded with scrollback: %q", got)
	}
	runCmd(cmd)
	if len(f.resumed) != 1 || f.resumed[0] != "arch" {
		t.Fatalf("archived choice must call Resume: %+v", f.resumed)
	}

	// A live session with an existing tab: Enter focuses it, no second Resume.
	m.openResume()
	for i, r := range m.resumeRows {
		if r.Name == "live" {
			m.resumeIdx = i
		}
	}
	cmd = m.chooseResume()
	if m.active != "cl" {
		t.Fatalf("live choice must focus its tab; active=%q", m.active)
	}
	runCmd(cmd)
	if len(f.resumed) != 1 {
		t.Fatalf("live choice must not call Resume: %+v", f.resumed)
	}
}

func TestClosedEventRemovesTab(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	m.ensureTab("b")
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "b"}, Event: contracts.Event{T: "closed"}})
	if _, ok := m.tabs["b"]; ok {
		t.Fatal("closed event must remove tab b")
	}
	for _, ch := range m.order {
		if ch == "b" {
			t.Fatal("closed tab still in order")
		}
	}
}

func TestSyncTabsFromSessions(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "alpha", ChannelID: "terminal/alpha-1"}}}
	m := newModel(f)
	m.syncTabs()
	tb, ok := m.tabs["terminal/alpha-1"]
	if !ok || tb.label != "alpha" {
		t.Fatalf("tab not synced/labelled from Sessions(): %+v", m.tabs)
	}
}

func TestTabBusyLifecycle(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "status", Text: "working"}})
	if !m.tabs["a"].busy {
		t.Fatal("status must mark tab busy")
	}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "done", Done: true, Cost: 0.01}})
	if m.tabs["a"].busy {
		t.Fatal("done reply must clear busy")
	}
	if m.tabs["a"].lastCost != 0.01 {
		t.Fatalf("lastCost = %v", m.tabs["a"].lastCost)
	}
}

func TestResizeSyncsViewport(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	m.tabs["a"].entries = []entry{{role: roleAgent, text: "line1"}, {role: roleAgent, text: "line2"}, {role: roleAgent, text: "line3"}}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// second size message exercises the resize (else) branch
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	// The full-width flow uses the whole width and reserves 3 chrome rows: the
	// status/spinner row, the composer (1), and the dim hint line.
	if m.vp.Width != 100 || m.vp.Height != 27 {
		t.Fatalf("resize: vp.Width=%d (want 100), vp.Height=%d (want 27)", m.vp.Width, m.vp.Height)
	}
}

func TestHelpReducesViewportHeight(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	base := m.vp.Height
	m.toggleHelp()
	if m.vp.Height != base-1 {
		t.Fatalf("shortcuts panel must shrink viewport by its one line: base=%d now=%d", base, m.vp.Height)
	}
	m.toggleHelp()
	if m.vp.Height != base {
		t.Fatalf("hiding help must restore viewport height: base=%d now=%d", base, m.vp.Height)
	}
}

func TestHelpToggle(t *testing.T) {
	m := newModel(&fakeBackend{})
	if m.showHelp {
		t.Fatal("help off by default")
	}
	m.toggleHelp()
	if !m.showHelp {
		t.Fatal("help must toggle on")
	}
}

func TestCloseActiveDispatchesClose(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	tb := m.ensureTab("terminal/alpha-1")
	tb.label = "alpha"
	m.active = "terminal/alpha-1"
	runCmd(m.confirmClose()) // simulate confirmed close
	if len(f.closed) != 1 {
		t.Fatalf("close not issued: %+v", f.closed)
	}
	if f.closed[0].name != "alpha" || f.closed[0].force {
		t.Fatalf("close mismatch: got %+v, want {alpha false}", f.closed[0])
	}
	if len(f.dispatched) != 0 {
		t.Fatalf("close must use the typed seam, not argv dispatch: %+v", f.dispatched)
	}
}

func TestAbandonedSetsDisconnected(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "abandoned"}})
	if !m.tabs["a"].disconnected {
		t.Fatal("abandoned must set disconnected")
	}
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "chunk", Text: "hi"}})
	if m.tabs["a"].disconnected {
		t.Fatal("chunk must clear disconnected")
	}
}

func TestQuestionMarkTogglesHelpWhenEmpty(t *testing.T) {
	m := newModel(&fakeBackend{})
	if m.showHelp {
		t.Fatal("help must start off")
	}
	// Send ? with empty input
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}
	m.Update(msg)
	if !m.showHelp {
		t.Fatal("? with empty input must toggle help on")
	}
	// Send ? again to toggle off
	m.Update(msg)
	if m.showHelp {
		t.Fatal("? with empty input must toggle help off")
	}
}

func TestQuestionMarkTypedWhenInputNonEmpty(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.input.SetValue("foo")
	if m.showHelp {
		t.Fatal("help must start off")
	}
	// Send ? with non-empty input
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}
	m.Update(msg)
	if m.showHelp {
		t.Fatal("? with non-empty input must not toggle help")
	}
	// Verify ? was typed into input (should have "foo?")
	if !strings.Contains(m.input.Value(), "?") {
		t.Fatalf("? must be typed into input: got %q", m.input.Value())
	}
}

// TestShortcutsPanelListsClaudeKeys checks the shortcuts panel names the Claude
// key affordances a returning user reaches for.
func TestShortcutsPanelListsClaudeKeys(t *testing.T) {
	m := newTestModel()
	panel := m.helpView()
	for _, want := range []string{"esc interrupt", "⌥⏎ newline", "/ commands", "@ files"} {
		if !strings.Contains(panel, want) {
			t.Fatalf("shortcuts panel missing %q: %q", want, panel)
		}
	}
}

// TestHistoryRecallsLastPrompt checks ↑ on an empty composer recalls the most
// recently submitted prompt.
func TestHistoryRecallsLastPrompt(t *testing.T) {
	f := &fakeBackend{}
	m := newModel(f)
	m.ensureTab("a")
	m.active = "a"
	m.ready = true
	m.input.SetValue("first message")
	runCmd(m.handleEnter())
	if m.input.Value() != "" {
		t.Fatalf("composer must clear after submit, got %q", m.input.Value())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if m.input.Value() != "first message" {
		t.Fatalf("↑ must recall last prompt, got %q", m.input.Value())
	}
}

// TestSpinnerHintShowsTokensAndCost checks the active-turn spinner renders the
// interrupt affordance, elapsed time, token count, and cost.
func TestSpinnerHintShowsTokensAndCost(t *testing.T) {
	m := newTestModel()
	tb := &tab{
		channel:   "a",
		busy:      true,
		tokens:    3400,
		lastCost:  0.02,
		startedAt: time.Now().Add(-5 * time.Second),
	}
	hint := m.spinnerHint(tb)
	for _, want := range []string{"esc to interrupt", "5s", "↑ 3.4k", "$0.02"} {
		if !strings.Contains(hint, want) {
			t.Fatalf("spinner hint missing %q: %q", want, hint)
		}
	}
}

func TestFormatTokens(t *testing.T) {
	cases := map[int]string{0: "0", 42: "42", 999: "999", 3400: "3.4k", 12000: "12.0k"}
	for in, want := range cases {
		if got := formatTokens(in); got != want {
			t.Fatalf("formatTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestSessionSwitchOpensAndFocuses checks /session switch opens the picker and
// Enter swaps the active transcript to the chosen session.
func TestSessionSwitchOpensAndFocuses(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{
		{Name: "one", ChannelID: "one"},
		{Name: "two", ChannelID: "two"},
	}}
	m := newModel(f)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.ensureTab("one")
	m.active = "one"
	m.input.SetValue("/session switch")
	runCmd(m.handleEnter())
	if !m.switchOpen {
		t.Fatal("/session switch must open the switcher")
	}
	m.switchIdx = 1 // select "two"
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.switchOpen {
		t.Fatal("switcher must close after a selection")
	}
	if m.active != "two" {
		t.Fatalf("switch must focus the chosen session, active = %q", m.active)
	}
}

// TestEscInterruptsActiveTurn checks esc emits an interrupt for the active
// session while a turn is in flight (instead of quitting).
func TestEscInterruptsActiveTurn(t *testing.T) {
	f := &fakeBackend{sessions: []contracts.SessionInfo{{Name: "a", ChannelID: "a"}}}
	m := newModel(f)
	m.ensureTab("a")
	m.active = "a"
	m.ready = true
	m.tabs["a"].busy = true
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if len(f.interrupted) != 1 || f.interrupted[0] != "a" {
		t.Fatalf("esc during a turn must interrupt the active session: %+v", f.interrupted)
	}
}

func TestPendingCloseCtrlCQuits(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.pendingClose = true
	if !m.pendingClose {
		t.Fatal("pendingClose must be true")
	}
	// Send Ctrl+C while pendingClose is true
	msg := tea.KeyMsg{Type: tea.KeyCtrlC}
	_, cmd := m.Update(msg)
	if m.pendingClose {
		t.Fatal("Ctrl+C during pendingClose must clear pendingClose")
	}
	// tea.Quit() is a function that returns a Cmd; we verify cmd is not nil (it's the quit command)
	if cmd == nil {
		t.Fatal("Ctrl+C during pendingClose must return a quit command")
	}
}

func TestPendingCloseEscQuits(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.pendingClose = true
	// Send Esc while pendingClose is true
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	_, cmd := m.Update(msg)
	if m.pendingClose {
		t.Fatal("Esc during pendingClose must clear pendingClose")
	}
	// tea.Quit() is a function that returns a Cmd; we verify cmd is not nil
	if cmd == nil {
		t.Fatal("Esc during pendingClose must return a quit command")
	}
}

func TestPendingCloseOtherKeyCancels(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.pendingClose = true
	// Send 'n' (any non-y key) while pendingClose is true
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")}
	_, cmd := m.Update(msg)
	if m.pendingClose {
		t.Fatal("non-y key during pendingClose must clear pendingClose")
	}
	if cmd != nil {
		t.Fatalf("non-y key during pendingClose must return nil cmd, got %v", cmd)
	}
}

// tabText joins a tab's logical entry bodies, for substring assertions that used
// to run over the old pre-rendered lines.
func tabText(tb *tab) string {
	parts := make([]string, len(tb.entries))
	for i, e := range tb.entries {
		parts[i] = e.text
	}
	return strings.Join(parts, "\n")
}

// tabLineCount counts how many entries contain needle in their body text.
func tabLineCount(tb *tab, needle string) int {
	n := 0
	for _, e := range tb.entries {
		if strings.Contains(e.text, needle) {
			n++
		}
	}
	return n
}

func TestSubmitMarksBusyImmediately(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	m.active = "a"
	m.input.SetValue("hello")
	runCmd(m.handleEnter())
	tb := m.tabs["a"]
	if !tb.busy {
		t.Fatal("submit must mark the tab busy immediately, before any backend event")
	}
	if tb.streamed {
		t.Fatal("submit must reset streamed so the thinking line shows until the first chunk")
	}
	if tabLineCount(tb, "hello") != 1 {
		t.Fatalf("the submitted line must be echoed exactly once: %+v", tb.entries)
	}
}

func TestStreamedReplyNotDuplicated(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	// A streamed turn: chunk carries the text, the final reply repeats it.
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "chunk", Text: "the answer"}})
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "the answer", Done: true}})
	if got := tabLineCount(m.tabs["a"], "the answer"); got != 1 {
		t.Fatalf("streamed reply must render once, not per-event: got %d\n%+v", got, m.tabs["a"].entries)
	}
	if m.tabs["a"].busy {
		t.Fatal("done reply must clear busy")
	}
}

func TestNonStreamedReplyRendersOnce(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	// No chunk first: the reply is the only carrier of the text.
	m.route(RoutedEvent{Conv: contracts.Conversation{ID: "a"}, Event: contracts.Event{T: "reply", Text: "direct answer", Done: true}})
	if got := tabLineCount(m.tabs["a"], "direct answer"); got != 1 {
		t.Fatalf("non-streamed reply must render exactly once: got %d\n%+v", got, m.tabs["a"].entries)
	}
}

func TestThinkingLineDerivedWhileBusyNoStream(t *testing.T) {
	m := newModel(&fakeBackend{})
	tb := m.ensureTab("a")
	tb.busy = true
	tb.streamed = false
	// The thinking indicator is derived at render time, never stored in lines.
	if tabLineCount(tb, "thinking") != 0 {
		t.Fatal("thinking indicator must not be appended to tab.lines")
	}
	if !strings.Contains(m.thinkingContent(), "thinking") {
		t.Fatal("thinkingContent must show the thinking line while busy and unstreamed")
	}
	tb.streamed = true
	if strings.Contains(m.thinkingContent(), "thinking") {
		t.Fatal("once streamed, the thinking line must disappear (the reply is arriving)")
	}
}

func TestAnyBusyReflectsTabs(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	m.ensureTab("b")
	if m.anyBusy() {
		t.Fatal("no tab busy → anyBusy false")
	}
	m.tabs["b"].busy = true
	if !m.anyBusy() {
		t.Fatal("one busy tab → anyBusy true")
	}
}

func TestPaletteOpensAndCompletes(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	m.active = "a"
	if m.paletteOpen() {
		t.Fatal("palette must be closed with empty input")
	}
	// Typing "/" opens the palette.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !m.paletteOpen() {
		t.Fatal("typing / must open the palette")
	}
	// Tab completes the selected command and pre-seeds its first flag, so the
	// operator lands on the value to type.
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.input.Value() != "/session create --name " {
		t.Fatalf("Tab must complete and pre-seed the flag: got %q", m.input.Value())
	}
}

func TestSpinnerStartsOnBusyStopsWhenIdle(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.ensureTab("a")
	m.active = "a"
	if m.spinning {
		t.Fatal("spinner must be idle at rest — no timer scheduled when nothing animates")
	}
	// Submitting a message goes busy and must start the animation timer.
	m.input.SetValue("hi")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.spinning || cmd == nil {
		t.Fatal("submit must flip busy and start the spinner")
	}
	// ensureSpin is idempotent: a concurrent busy event must not stack a timer.
	if m.ensureSpin() != nil {
		t.Fatal("ensureSpin must not schedule a second ticker while one runs")
	}
	// The turn completes; the next spin tick must stop the timer rather than reschedule.
	m.tabs["a"].busy = false
	_, cmd = m.Update(spinMsg{})
	if m.spinning || cmd != nil {
		t.Fatal("spinMsg while idle must stop the timer, scheduling no further frames")
	}
}

func TestPaletteEscClosesWithoutQuitting(t *testing.T) {
	m := newModel(&fakeBackend{})
	m.input.SetValue("/session")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatal("Esc while the palette is open must close it, not quit")
	}
	if m.paletteOpen() {
		t.Fatalf("Esc must clear the query: got %q", m.input.Value())
	}
}
