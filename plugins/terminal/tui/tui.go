// Package tui renders a gateway's live event stream and captures the operator's
// input, driving it through the narrow Backend interface. It is the terminal
// gateway's frontend: the daemon hub treats that gateway like any other; the TUI
// is what makes it a human-usable pane. Depending on Backend (not the concrete
// gateway) keeps this package importable by the gateway without a cycle.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// RoutedEvent is a turn event tagged with the conversation (session channel) it
// belongs to, so the TUI can route it to the right tab.
type RoutedEvent struct {
	Conv  contracts.Conversation
	Event contracts.Event
}

// Backend is the narrow view of the terminal gateway the TUI drives: it reads
// routed outbound events to render, submits the lines the operator types into a
// specific channel, enumerates the hub's sessions for tab labels, and dispatches
// operator slash-commands to the hub. Taking an interface keeps this package
// free of any dependency on the terminal plugin.
type Backend interface {
	Frontend() <-chan RoutedEvent
	Submit(channel, text string)
	Sessions() []contracts.SessionInfo
	Dispatch(args []string) (string, error)
	// Close tears a session down by name through the typed control seam, so the
	// UI's close action never assembles "session close" flag argv.
	Close(name string, force bool) (string, error)
	// Commands lists the operator commands the palette advertises, scoped to the
	// verbs this backend will actually accept.
	Commands() []CommandSpec
}

// tab is one session's pane: its transcript, unread flag, busy state, last cost,
// and a disconnected flag set when the last event was an "abandoned" turn. streamed
// records whether any chunk arrived during the current turn, so the final reply is
// not rendered a second time (the duplicate fix) and the "thinking" indicator hides
// once real output begins.
type tab struct {
	channel      string
	label        string
	lines        []string
	unread       bool
	busy         bool
	streamed     bool
	lastCost     float64
	disconnected bool
}

// maxTabLines bounds a tab's transcript so a long-lived or chatty session
// (streaming chunk events token-by-token) cannot grow memory without limit.
const maxTabLines = 5000

// appendLine adds a rendered line to the tab, dropping the oldest lines once the
// transcript exceeds maxTabLines. Trimming reallocates so the backing array does
// not pin the whole history in memory.
func (tb *tab) appendLine(s string) {
	tb.lines = append(tb.lines, s)
	if len(tb.lines) > maxTabLines {
		tb.lines = append([]string(nil), tb.lines[len(tb.lines)-maxTabLines:]...)
	}
}

// dispatchResultMsg carries the outcome of a slash-command run off the Update
// goroutine back into it, so a slow command never freezes the TUI.
type dispatchResultMsg struct {
	origin string
	out    string
	err    error
}

type eventMsg RoutedEvent

// tickMsg fires on a periodic timer so the TUI refreshes tabs from the hub.
type tickMsg struct{}

// tickCmd returns a command that fires tickMsg after ~1 second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// spinMsg fires the animation timer that advances the "thinking" spinner. It
// runs fast (fastTick) while a session is busy and slow otherwise, so an idle
// TUI is not repainting several times a second.
type spinMsg struct{}

const fastTick = 200 * time.Millisecond

// spinTick returns a command that fires spinMsg after d.
func spinTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return spinMsg{} })
}

type model struct {
	tm           Backend
	vp           viewport.Model
	input        textinput.Model
	tabs         map[string]*tab
	order        []string
	active       string
	ready        bool
	showHelp     bool
	pendingClose bool
	width        int
	height       int
	flash        string        // transient status shown when there is no active tab
	tabSig       string        // last reconciled session signature, to skip idle work
	spin         int           // animation frame for the thinking spinner
	cmds         []CommandSpec // palette command table, loaded from the backend
	palIdx       int           // selected row in the open command palette
	palWasOpen   bool          // palette open-state after the previous key, to skip idle re-fits
	spinning     bool          // whether the animation timer is currently running
}

// chromeHeight is the number of non-viewport rows View renders: the brand row, the
// tab strip, the footer status line, and the input row (4), plus the help block and
// the command palette when each is shown.
func (m *model) chromeHeight() int {
	h := 6 // panel border (top+bottom) + brand row + tab strip + footer + input
	if m.showHelp {
		h += 4
	}
	if m.paletteOpen() {
		h += m.paletteHeight()
	}
	return h
}

// innerWidth is the usable width inside the panel: the window minus the border
// and horizontal padding lipgloss adds on each side. Every width-aligned row
// (brand, tabs, input) and the viewport are sized to it so nothing overruns the
// frame. Floored at 1 so a tiny terminal never yields a negative width.
func (m *model) innerWidth() int {
	w := m.width - 4
	if w < 1 {
		w = 1
	}
	return w
}

// applySize fits the viewport to the current window minus the chrome.
func (m *model) applySize() {
	if !m.ready {
		return
	}
	m.vp.Width = m.innerWidth()
	m.vp.Height = m.height - m.chromeHeight()
}

func newModel(tm Backend) *model {
	in := textinput.New()
	in.Placeholder = "type a message…"
	in.Focus()
	m := &model{tm: tm, input: in, tabs: map[string]*tab{}}
	if tm != nil {
		m.cmds = tm.Commands()
	}
	return m
}

// ensureTab creates a tab for channel if missing, making the first tab active.
func (m *model) ensureTab(channel string) *tab {
	if tb, ok := m.tabs[channel]; ok {
		return tb
	}
	tb := &tab{channel: channel, label: channel}
	m.tabs[channel] = tb
	m.order = append(m.order, channel)
	if m.active == "" {
		m.active = channel
	}
	return tb
}

// removeTab drops a tab and fixes the active selection.
func (m *model) removeTab(channel string) {
	if _, ok := m.tabs[channel]; !ok {
		return
	}
	delete(m.tabs, channel)
	out := m.order[:0]
	for _, ch := range m.order {
		if ch != channel {
			out = append(out, ch)
		}
	}
	m.order = out
	if m.active == channel {
		m.active = ""
		if len(m.order) > 0 {
			m.active = m.order[0]
		}
		m.syncViewport()
	}
}

// route delivers a routed event to its tab, marking inactive tabs unread.
func (m *model) route(re RoutedEvent) {
	if re.Event.T == "closed" {
		m.removeTab(re.Conv.ID)
		return
	}
	tb := m.ensureTab(re.Conv.ID)
	before := len(tb.lines)
	m.renderInto(tb, re.Event)
	if len(tb.lines) != before && tb.channel != m.active {
		tb.unread = true
	}
	if tb.channel == m.active {
		m.syncViewport()
	}
}

// syncTabs reconciles tabs against the hub's session list: it creates tabs for
// new sessions, labels them by name, and drops tabs whose session is gone.
func (m *model) syncTabs() {
	infos := m.tm.Sessions()
	if infos == nil {
		return
	}
	var sb strings.Builder
	for _, s := range infos {
		sb.WriteString(s.ChannelID)
		sb.WriteByte('\x1f')
		sb.WriteString(s.Name)
		sb.WriteByte('\n')
	}
	sig := sb.String()
	if sig == m.tabSig {
		return // session set unchanged since last reconcile
	}
	m.tabSig = sig
	live := map[string]bool{}
	for _, s := range infos {
		live[s.ChannelID] = true
		tb := m.ensureTab(s.ChannelID)
		if s.Name != "" {
			tb.label = s.Name
		}
	}
	for _, ch := range append([]string(nil), m.order...) {
		if !live[ch] {
			m.removeTab(ch)
		}
	}
}

// handleEnter dispatches a /command or submits a prompt to the active tab. A
// slash command runs in a returned tea.Cmd (off the Update goroutine) so a slow
// dispatch — e.g. a clone against an unreachable host — never freezes the TUI;
// its result is delivered back as a dispatchResultMsg.
func (m *model) handleEnter() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.input.Reset()
	if strings.HasPrefix(text, "/") {
		args := strings.Fields(strings.TrimPrefix(text, "/"))
		if len(args) == 0 {
			return nil // a bare "/" is the palette prefix, not an empty command to run
		}
		return m.dispatchCmd(m.active, args)
	}
	if m.active == "" {
		return nil
	}
	m.tm.Submit(m.active, text)
	tb := m.tabs[m.active]
	tb.appendLine(humanStyle.Render(glyphYou+" you ") + text)
	// Flip to the working state immediately, before any backend event, so the
	// operator sees the message was taken (the "thinking" line is derived from this).
	tb.busy = true
	tb.streamed = false
	m.syncViewport()
	return nil
}

// dispatchCmd runs an operator argv against the backend off the Update goroutine,
// tagging the result with the tab it was issued from.
func (m *model) dispatchCmd(origin string, args []string) tea.Cmd {
	tm := m.tm
	return func() tea.Msg {
		out, err := tm.Dispatch(args)
		return dispatchResultMsg{origin: origin, out: out, err: err}
	}
}

// toggleHelp flips the help overlay on/off, resizing the viewport so the help
// block does not push the chrome off-screen.
func (m *model) toggleHelp() {
	m.showHelp = !m.showHelp
	m.applySize()
	m.syncViewport()
}

// sessionName resolves a channel id to its logical session name via the hub,
// returning "" when the session is not (yet) known.
func (m *model) sessionName(channel string) string {
	for _, s := range m.tm.Sessions() {
		if s.ChannelID == channel && s.Name != "" {
			return s.Name
		}
	}
	return ""
}

// confirmClose dispatches a close for the active tab's session, resolving the
// real session name (the tab label can still be the channel id before the first
// reconcile) and surfacing any error through dispatchResultMsg.
func (m *model) confirmClose() tea.Cmd {
	tb := m.tabs[m.active]
	if tb == nil {
		return nil
	}
	name := m.sessionName(tb.channel)
	if name == "" {
		name = tb.label
	}
	origin, tm := m.active, m.tm
	return func() tea.Msg {
		out, err := tm.Close(name, false)
		return dispatchResultMsg{origin: origin, out: out, err: err}
	}
}

func (m *model) switchTab(delta int) {
	if len(m.order) == 0 {
		return
	}
	idx := 0
	for i, ch := range m.order {
		if ch == m.active {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(m.order)) % len(m.order)
	m.active = m.order[idx]
	m.tabs[m.active].unread = false
	m.syncViewport()
	m.vp.GotoBottom() // a freshly switched-to tab starts at its latest output
}

func (m *model) renderInto(tb *tab, e contracts.Event) {
	// Any non-abandoned event clears the disconnected marker.
	if e.T != "abandoned" {
		tb.disconnected = false
	}
	switch e.T {
	case "chunk":
		tb.busy = true
		tb.streamed = true
		tb.appendLine(e.Text)
	case "status":
		tb.busy = true
		tb.appendLine(statusStyle.Render("· " + e.Text))
	case "reply":
		// A streamed answer is already on screen from its chunks; rendering the
		// final reply text again is the duplicate we are killing. The streamed
		// flag holds whether or not the reply repeats the text, so a
		// non-streaming backend still renders reply.Text exactly once.
		if e.Text != "" && !tb.streamed {
			tb.appendLine(replyStyle.Render(e.Text))
		}
		if e.Cost > 0 {
			tb.lastCost = e.Cost
			tb.appendLine(costStyle.Render(formatCost(e.Cost)))
		}
		if e.Done {
			tb.busy = false
			tb.streamed = false
		}
	case "reset":
		tb.busy = false
		tb.streamed = false
		tb.appendLine(statusStyle.Render("· (turn reset)"))
	case "abandoned":
		tb.busy = false
		tb.streamed = false
		tb.disconnected = true
		tb.appendLine(statusStyle.Render("· (turn abandoned)"))
	}
}

func (m *model) syncViewport() {
	if !m.ready {
		return
	}
	if m.tabs[m.active] == nil {
		m.vp.SetContent("")
		return
	}
	// Preserve the operator's scroll position: only snap to the bottom when the
	// view is already there, so streaming output into the active tab does not
	// defeat PgUp/PgDn scrollback mid-turn.
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(m.thinkingContent())
	if atBottom {
		m.vp.GotoBottom()
	}
}

// anyBusy reports whether any tab has a turn in flight, so the spinner can idle.
func (m *model) anyBusy() bool {
	for _, tb := range m.tabs {
		if tb.busy {
			return true
		}
	}
	return false
}

func (m *model) spinFrame() string { return spinFrames[m.spin%len(spinFrames)] }

// thinkingContent is the active tab's rendered transcript plus a derived, non-stored
// "thinking" line, shown only when the turn is accepted but nothing has streamed back
// yet. Because it is derived from state it appears the instant Enter is pressed and
// disappears when the first chunk lands or the turn completes — it can never double.
func (m *model) thinkingContent() string {
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	content := strings.Join(tb.lines, "\n")
	if tb.busy && !tb.streamed {
		line := workingStyle.Render(m.spinFrame() + " thinking…")
		if content != "" {
			content += "\n" + line
		} else {
			content = line
		}
	}
	return content
}

// totalCost sums the last per-turn cost across tabs, for the brand-row summary.
func (m *model) totalCost() float64 {
	var sum float64
	for _, tb := range m.tabs {
		sum += tb.lastCost
	}
	return sum
}

// brandRow renders the brand on the left and the session/cost summary on the right.
func (m *model) brandRow() string {
	left := brandStyle.Render(glyphBrand + " HERRSCHER")
	noun := "sessions"
	if len(m.order) == 1 {
		noun = "session"
	}
	right := statusStyle.Render(fmt.Sprintf("%d %s · %s", len(m.order), noun, formatCost(m.totalCost())))
	gap := m.innerWidth() - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

// tabStrip renders the session tabs: active tab highlighted with the brand glyph,
// unread marked with •, busy with ⟳, left-truncated around the active tab when wide.
func (m *model) tabStrip() string {
	if len(m.order) == 0 {
		return statusStyle.Render("no sessions — /session create <name>")
	}
	segs := make([]string, len(m.order))
	active := 0
	for i, ch := range m.order {
		tb := m.tabs[ch]
		marker := "  "
		if tb.busy {
			marker = workingStyle.Render(glyphBusy) + " "
		} else if tb.unread {
			marker = unreadStyle.Render(glyphUnread) + " "
		}
		if ch == m.active {
			active = i
			segs[i] = marker + activeTabStyle.Render(glyphBrand+" "+tb.label) + " "
		} else {
			segs[i] = marker + inactiveTabStyle.Render(tb.label) + " "
		}
	}
	return fitTabs(segs, active, m.innerWidth())
}

// fitTabs joins tab segments, dropping whole tabs from the front (never past the
// active one) until the active tab fits the width; a leading ‹ marks elision.
func fitTabs(segs []string, active, width int) string {
	start := 0
	for {
		joined := strings.Join(segs[start:], "")
		prefix := ""
		if start > 0 {
			prefix = statusStyle.Render("‹ ")
		}
		if lipgloss.Width(prefix)+lipgloss.Width(joined) <= width || start >= active {
			return prefix + joined
		}
		start++
	}
}

// inputRow renders the prompt with a right-aligned command hint. The hint switches
// to palette navigation keys while the palette is open.
func (m *model) inputRow() string {
	hint := statusStyle.Render("/ commands · ? help · Tab ⇄")
	if m.paletteOpen() {
		hint = statusStyle.Render("↑↓ navigate · Tab complete · Esc close")
	}
	in := m.input.View()
	gap := m.innerWidth() - lipgloss.Width(in) - lipgloss.Width(hint)
	if gap < 1 {
		return in
	}
	return in + strings.Repeat(" ", gap) + hint
}

// footer renders the status/cost line for the active tab.
func (m *model) footer() string {
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	if tb.disconnected {
		return statusStyle.Render("· disconnected")
	}
	state := statusStyle.Render("· idle")
	if tb.busy {
		return workingStyle.Render(glyphBusy + " working…")
	}
	cost := ""
	if tb.lastCost > 0 {
		cost = "  " + costStyle.Render("last "+formatCost(tb.lastCost))
	}
	return state + cost
}

// formatCost renders a turn's USD cost, matching the host progress summary:
// sub-cent costs get four decimals, larger ones two.
func formatCost(c float64) string {
	if c < 0.01 {
		return fmt.Sprintf("$%.4f", c)
	}
	return fmt.Sprintf("$%.2f", c)
}

// Run starts the TUI bound to the given gateway backend, blocking until the user
// quits; quitting cancels ctx (wired by the caller) so the daemon shuts down
// cleanly.
func Run(ctx context.Context, cancel context.CancelFunc, tm Backend) error {
	m := newModel(tm)
	p := tea.NewProgram(m, tea.WithAltScreen())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-tm.Frontend():
				if !ok {
					return
				}
				p.Send(eventMsg(e))
			}
		}
	}()
	_, err := p.Run()
	cancel()
	return err
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickCmd())
}

// ensureSpin starts the animation timer if it is not already running. A turn
// that just went busy calls it to begin animating; the guard means overlapping
// events never stack duplicate tickers. The timer stops itself once idle, so an
// idle TUI schedules no spin frames at all.
func (m *model) ensureSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return spinTick(fastTick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-m.chromeHeight())
			m.ready = true
		} else {
			m.applySize()
		}
		m.input.Width = m.innerWidth() - 2
		m.syncViewport()
	case tea.KeyMsg:
		m.flash = "" // any keypress clears a transient status
		// Two-step close confirm: if waiting for confirmation, next key decides.
		if m.pendingClose {
			if msg.String() == "y" {
				cmd := m.confirmClose()
				m.pendingClose = false
				return m, cmd
			}
			// Quit keys always quit, even during pending close.
			if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
				m.pendingClose = false
				return m, tea.Quit
			}
			// Any other key cancels the close.
			m.pendingClose = false
			return m, nil
		}
		// While the command palette is open, arrow/Tab/Esc/Enter drive it instead
		// of the normal bindings; other keys fall through to edit the query.
		if m.paletteOpen() {
			switch msg.Type {
			case tea.KeyUp:
				m.movePal(-1)
				return m, nil
			case tea.KeyDown:
				m.movePal(1)
				return m, nil
			case tea.KeyTab:
				m.completePal()
				m.applySize()
				m.syncViewport()
				return m, nil
			case tea.KeyEsc:
				m.input.SetValue("")
				m.palIdx = 0
				m.applySize()
				m.syncViewport()
				return m, nil
			case tea.KeyEnter:
				cmd := m.handleEnter()
				m.palIdx = 0
				m.applySize()
				m.syncViewport()
				return m, tea.Batch(cmd, m.ensureSpin())
			case tea.KeyCtrlC:
				return m, tea.Quit
			}
		}
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyTab:
			m.switchTab(1)
			return m, nil
		case tea.KeyShiftTab:
			m.switchTab(-1)
			return m, nil
		case tea.KeyEnter:
			return m, tea.Batch(m.handleEnter(), m.ensureSpin())
		case tea.KeyCtrlW:
			m.pendingClose = true
			return m, nil
		case tea.KeyRunes:
			if msg.String() == "?" && m.input.Value() == "" {
				m.toggleHelp()
				return m, nil
			}
		}
		// PgUp/PgDn reach m.vp.Update(msg) below — not intercepted here.
	case eventMsg:
		m.route(RoutedEvent(msg))
		// A chunk/status event may have flipped a tab busy; start animating.
		return m, m.ensureSpin()
	case dispatchResultMsg:
		m.syncTabs()
		line := msg.out
		if msg.err != nil {
			line = "error: " + msg.err.Error()
		}
		if line != "" {
			tb := m.tabs[msg.origin]
			if tb == nil {
				tb = m.tabs[m.active]
			}
			if tb != nil {
				tb.appendLine(statusStyle.Render("· " + line))
				m.syncViewport()
			} else {
				m.flash = line // no tab to render into — surface it standalone
			}
		}
		return m, nil
	case tickMsg:
		m.syncTabs()
		return m, tickCmd()
	case spinMsg:
		if !m.anyBusy() {
			m.spinning = false // nothing to animate; let the timer lapse
			return m, nil
		}
		m.spin++
		m.syncViewport() // repaint the animated thinking line
		return m, spinTick(fastTick)
	}
	var cmds []tea.Cmd
	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	cmds = append(cmds, c)
	m.vp, c = m.vp.Update(msg)
	cmds = append(cmds, c)
	// Editing the query can open/close the palette or change its match count,
	// both of which change the reserved chrome height. Re-fit only while the
	// palette is (or was just) open, so typing an ordinary message does not
	// rebuild the viewport on every keystroke.
	if _, ok := msg.(tea.KeyMsg); ok {
		if m.paletteOpen() || m.palWasOpen {
			m.clampPal()
			m.applySize()
			m.syncViewport()
		}
		m.palWasOpen = m.paletteOpen()
	}
	return m, tea.Batch(cmds...)
}

// helpView returns the key-binding cheat-sheet rendered as a faint status block.
func (m *model) helpView() string {
	lines := []string{
		"Keys:  Tab / Shift+Tab  switch tab        Ctrl+W  close tab (y to confirm)",
		"       PgUp / PgDn      scroll             ?       toggle this help",
		"       /                command palette    Enter   submit / run /cmd",
		"       ↑↓ Tab Esc       navigate palette   Ctrl+C  quit",
	}
	return statusStyle.Render(strings.Join(lines, "\n"))
}

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	footer := m.footer()
	if m.flash != "" {
		footer = statusStyle.Render("· " + m.flash)
	}
	parts := []string{m.brandRow(), m.tabStrip(), m.vp.View()}
	if m.paletteOpen() {
		parts = append(parts, m.paletteView())
	}
	if m.showHelp {
		parts = append(parts, m.helpView())
	}
	parts = append(parts, footer, m.inputRow())
	// The style width counts padding but not the border; width-2 gives a content
	// wrap of innerWidth (each row is built to that) and a total of the full width.
	return panelBorder.Width(m.width - 2).Render(strings.Join(parts, "\n"))
}
