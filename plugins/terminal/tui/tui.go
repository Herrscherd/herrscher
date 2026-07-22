// Package tui renders a gateway's live event stream and captures the operator's
// input, driving it through the narrow Backend interface. It is the terminal
// gateway's frontend: the daemon hub treats that gateway like any other; the TUI
// is what makes it a human-usable pane. Depending on Backend (not the concrete
// gateway) keeps this package importable by the gateway without a cycle.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textarea"
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
	Submit(channel, text string, attachments []Attachment)
	Sessions() []contracts.SessionInfo
	Dispatch(args []string) (string, error)
	// Close tears a session down by name through the typed control seam, so the
	// UI's close action never assembles "session close" flag argv.
	Close(name string, force bool) (string, error)
	// Commands lists the operator commands the palette advertises, scoped to the
	// verbs this backend will actually accept.
	Commands() []CommandSpec
	// Scrollback returns a session's recorded history lines, so a reopened tab is
	// seeded with the conversation before live events arrive. Empty when none.
	Scrollback(name string) []contracts.ScrollbackLine
	// Resume revives an archived session by name (backend resumed via its stored
	// token), for the /resume picker. Returns a human-readable result or an error.
	Resume(name string) (string, error)
}

// roles label a transcript entry so the renderer maps it to a gutter + body style.
const (
	roleYou        = "you"
	roleAgent      = "agent"
	roleStatus     = "status"
	roleCost       = "cost"
	roleScrollback = "scrollback"
)

// entry is one logical unit of a tab's transcript: a role plus the unwrapped,
// unstyled body text. Storing logical text (not pre-rendered lines) is what lets
// the renderer re-wrap to the current width on every resize and coalesce a
// streamed answer's chunks into one wrappable block. streaming marks an agent
// entry still being extended by chunk events.
type entry struct {
	role        string
	text        string
	streaming   bool
	attachments []Attachment // files echoed under a user turn (chips)
	preview     string       // precomputed kitty graphics escape for image attachments; "" when unsupported or none
}

// tab is one session's pane: its transcript, unread flag, busy state, last cost,
// and a disconnected flag set when the last event was an "abandoned" turn. streamed
// records whether any chunk arrived during the current turn, so the final reply is
// not rendered a second time (the duplicate fix) and the "thinking" indicator hides
// once real output begins.
type tab struct {
	channel      string
	label        string
	entries      []entry
	unread       bool
	busy         bool
	streamed     bool
	lastCost     float64
	disconnected bool
}

// maxTabLines bounds the number of logical entries a tab's transcript retains so
// a long-lived session cannot grow memory without limit. It caps entry count, not
// the size of any one entry: a streamed answer coalesces into a single entry (see
// appendChunk) whose text grows for the duration of that turn.
const maxTabLines = 5000

// appendEntry adds a logical entry, dropping the oldest once the transcript
// exceeds maxTabLines. Trimming reallocates so the backing array does not pin the
// whole history in memory.
func (tb *tab) appendEntry(e entry) {
	tb.entries = append(tb.entries, e)
	if len(tb.entries) > maxTabLines {
		tb.entries = append([]entry(nil), tb.entries[len(tb.entries)-maxTabLines:]...)
	}
}

// appendChunk coalesces streamed agent prose: it extends the current live agent
// entry, or opens a new one when the previous entry is not an in-flight stream
// (e.g. a status line interrupted it). The final wrap is thus over the whole
// message, not per-token fragments.
func (tb *tab) appendChunk(text string) {
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent && tb.entries[n-1].streaming {
		tb.entries[n-1].text += text
		return
	}
	tb.appendEntry(entry{role: roleAgent, text: text, streaming: true})
}

// endStream clears the streaming flag on the current agent entry so later chunks
// (a new turn) open a fresh block instead of extending a finished answer.
func (tb *tab) endStream() {
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent {
		tb.entries[n-1].streaming = false
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
	input        textarea.Model
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

	resumeOpen bool                    // whether the /resume picker overlay is open
	resumeIdx  int                     // selected row in the /resume picker
	resumeRows []contracts.SessionInfo // picker rows, sorted by LastTs desc

	switchOpen bool                    // whether the /session switch picker is open
	switchIdx  int                     // selected row in the switch picker
	switchRows []contracts.SessionInfo // switch picker rows (open/known sessions)

	choice    *PendingChoice // an active allow/deny permission menu, else nil
	choiceIdx int            // selected row in the permission menu

	mentionIdx int // selected row in the inline @ file-mention list

	history []string // ring of submitted prompts, for ↑/↓ recall
	histIdx int      // cursor into history while recalling (len(history) == not recalling)

	clip      clipboard    // system clipboard reader for Ctrl+V image paste
	pending   []Attachment // files staged for the next submit, shown as chips
	attachSeq int          // monotonic counter for naming pasted temp files
	kitty     bool         // terminal renders the kitty graphics protocol (inline image previews)

	// tsCache memoizes the active tab's wrapped transcript so the animation tick
	// (which repaints every fastTick while a turn is busy) does not re-wrap the
	// whole history on each frame — only a real content or width change does.
	tsCache transcriptCache
}

// transcriptCache holds the last rendered transcript and the key it was rendered
// for. The key captures everything renderTranscript's output depends on: which
// tab, the wrap width, the entry count, and the length + streaming state of the
// last entry (the only one appendChunk mutates in place). A matching key means
// the wrapped output is byte-identical, so it can be reused without re-wrapping.
type transcriptCache struct {
	ch      string
	width   int
	entries int
	lastLen int
	lastStr bool
	out     string
	valid   bool
}

// maxComposerLines caps how tall the multi-line composer grows before it scrolls
// internally, so a long draft never eats the whole transcript.
const maxComposerLines = 8

// composerHeight is the current height of the input composer in rows (≥1). It
// grows with the draft up to maxComposerLines.
func (m *model) composerHeight() int {
	if h := m.input.Height(); h >= 1 {
		return h
	}
	return 1
}

// resizeComposer grows the composer with its content up to maxComposerLines and
// re-fits the viewport whenever that height changes, so the transcript never
// overlaps a multi-line draft.
func (m *model) resizeComposer() {
	h := m.input.LineCount()
	if h < 1 {
		h = 1
	}
	if h > maxComposerLines {
		h = maxComposerLines
	}
	if h != m.input.Height() {
		m.input.SetHeight(h)
		m.applySize()
		m.syncViewport()
	}
}

// chromeHeight is the number of non-viewport rows View renders. The Claude flow
// has no enclosing card and no tab strip: the fixed chrome is the status/spinner
// row and the dim hint line (2), plus the composer's current height, and any
// staged-chip row, shortcuts line, palette, or picker when each is shown.
func (m *model) chromeHeight() int {
	h := 2 + m.composerHeight()
	if len(m.pending) > 0 {
		h++ // the staged-attachments chip row
	}
	if m.showHelp {
		h++ // the one-line shortcuts panel
	}
	if m.paletteOpen() {
		h += m.paletteHeight()
	}
	if m.mentionOpen() {
		h += m.mentionHeight()
	}
	if m.resumeOpen {
		h += m.resumeHeight()
	}
	if m.switchOpen {
		h += m.switchHeight()
	}
	if m.choice != nil {
		h += m.choiceHeight()
	}
	return h
}

// innerWidth is the usable content width: the full window (no border, no card),
// floored at 1 so a tiny terminal never yields a negative width.
func (m *model) innerWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
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
	in := textarea.New()
	in.Placeholder = "type a message…"
	in.Prompt = "" // the composer sits flush inside the panel; no per-line gutter
	in.ShowLineNumbers = false
	// Enter submits (intercepted in Update); a newline is an explicit Alt+Enter or
	// Ctrl+J, since Shift+Enter is not reliably distinguishable across terminals.
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	in.SetHeight(1)
	in.Focus()
	m := &model{tm: tm, input: in, tabs: map[string]*tab{}, clip: newClipboard(), kitty: supportsKitty(os.Getenv)}
	// Seed the palette with the Claude command set; a backend that advertises its
	// own verbs overrides it.
	m.cmds = defaultCommands()
	if tm != nil {
		if bc := tm.Commands(); len(bc) > 0 {
			m.cmds = bc
		}
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
	m.seedScrollback(tb)
	return tb
}

// seedScrollback fills a freshly created tab with its recorded transcript, dimmed
// so replayed history reads as past context distinct from live output. It is a
// no-op when the backend is absent, the session name is not yet known, or no
// history exists — a reopened tab is thus seeded before any live event arrives.
func (m *model) seedScrollback(tb *tab) {
	if m.tm == nil {
		return
	}
	name := m.sessionName(tb.channel)
	if name == "" {
		return
	}
	for _, ln := range m.tm.Scrollback(name) {
		tb.appendEntry(entry{role: roleScrollback, text: scrollbackPrefix(ln.Role) + ln.Text})
	}
}

// scrollbackPrefix labels a replayed line by its role, mirroring the live render
// (a dim "> " prompt for user turns, bare prose for the agent) so seeded history
// looks like the conversation it replays.
func scrollbackPrefix(role string) string {
	switch role {
	case "user":
		return glyphPrompt + " "
	default:
		return ""
	}
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
	before := len(tb.entries)
	m.renderInto(tb, re.Event)
	if len(tb.entries) != before && tb.channel != m.active {
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
		live[s.ChannelID] = true // keep an already-open tab even once archived
		if s.Archived {
			continue // archived sessions never auto-open a tab (reopen via /resume)
		}
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
	if text == "" && len(m.pending) == 0 {
		return nil
	}
	m.input.Reset()
	m.resizeComposer() // a submitted draft collapses the composer back to one row
	if strings.HasPrefix(text, "/") {
		args := strings.Fields(strings.TrimPrefix(text, "/"))
		if len(args) == 0 {
			return nil // a bare "/" is the palette prefix, not an empty command to run
		}
		if args[0] == "resume" {
			m.openResume() // TUI-local overlay, not a backend command
			return nil
		}
		if args[0] == "clear" {
			m.clearActive() // TUI-local: wipe the active transcript
			return nil
		}
		if args[0] == "help" {
			m.toggleHelp() // TUI-local: the shortcuts panel
			return nil
		}
		if args[0] == "session" && len(args) >= 2 && args[1] == "switch" {
			m.openSwitch() // TUI-local session switcher (invisible multi-session)
			return nil
		}
		if args[0] == "attach" {
			m.stageAttachment(strings.TrimSpace(strings.TrimPrefix(text, "/attach")))
			return nil
		}
		return m.dispatchCmd(m.active, args)
	}
	if m.active == "" {
		return nil
	}
	atts := m.pending
	m.pending = nil
	m.applySize() // the chip row (if any) is gone now the draft is sent
	m.recordHistory(text)
	m.tm.Submit(m.active, text, atts)
	tb := m.tabs[m.active]
	tb.endStream() // a new user turn closes any lingering agent block
	e := entry{role: roleYou, text: text, attachments: atts}
	if m.kitty {
		e.preview = previewEscapes(atts) // inline image previews under the chips
	}
	tb.appendEntry(e)
	// Flip to the working state immediately, before any backend event, so the
	// operator sees the message was taken (the "thinking" line is derived from this).
	tb.busy = true
	tb.streamed = false
	m.syncViewport()
	return nil
}

// pasteImage pulls an image off the system clipboard, stages it for the next
// submit, and reports whether it consumed the paste. A clipboard holding no image
// (or no clipboard tool) returns false, so Ctrl+V falls through to a text paste.
func (m *model) pasteImage() bool {
	if m.clip == nil {
		return false
	}
	mime, ok := m.clip.ImageType()
	if !ok {
		return false
	}
	data, err := m.clip.ReadImage(mime)
	if err != nil {
		m.flash = "paste failed: " + err.Error()
		return true // an image was on the clipboard; do not fall through to text
	}
	att, err := saveClipboardImage(data, mime, m.attachSeq)
	if err != nil {
		m.flash = err.Error()
		return true
	}
	m.attachSeq++
	m.pending = append(m.pending, att)
	m.applySize() // the new chip row steals a viewport line
	m.syncViewport()
	return true
}

// stageAttachment resolves a /attach path to a staged file, surfacing any error
// through the transient flash rather than the transcript.
func (m *model) stageAttachment(path string) {
	att, err := attachLocalFile(path)
	if err != nil {
		m.flash = err.Error()
		return
	}
	m.pending = append(m.pending, att)
	m.applySize()
	m.syncViewport()
}

// removeLastPending drops the most recently staged attachment, returning whether
// one was there to drop — so the Ctrl+U binding can fall through to the composer's
// delete-to-line-start when nothing is staged.
func (m *model) removeLastPending() bool {
	if len(m.pending) == 0 {
		return false
	}
	m.pending = m.pending[:len(m.pending)-1]
	return true
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

// recordHistory appends a submitted prompt to the input-history ring and resets
// the recall cursor to the end, so the next ↑ starts from the newest entry.
func (m *model) recordHistory(text string) {
	if text == "" {
		return
	}
	// Skip a duplicate of the most recent entry so repeated sends don't stack.
	if n := len(m.history); n == 0 || m.history[n-1] != text {
		m.history = append(m.history, text)
	}
	m.histIdx = len(m.history)
}

// recallHistory moves the recall cursor by d (−1 = older, +1 = newer) and loads
// that prompt into the composer. Moving past the newest entry restores an empty
// draft. It reports whether it consumed the key (false when there is no history).
func (m *model) recallHistory(d int) bool {
	if len(m.history) == 0 {
		return false
	}
	idx := m.histIdx + d
	if idx < 0 {
		idx = 0
	}
	if idx >= len(m.history) {
		m.histIdx = len(m.history)
		m.input.SetValue("")
		return true
	}
	m.histIdx = idx
	m.input.SetValue(m.history[idx])
	m.input.CursorEnd()
	return true
}

// clearActive wipes the active tab's transcript, the /clear conversation reset.
// It leaves the session itself untouched — only the on-screen history is cleared.
func (m *model) clearActive() {
	tb := m.tabs[m.active]
	if tb == nil {
		return
	}
	tb.entries = nil
	tb.streamed = false
	m.tsCache.valid = false
	m.syncViewport()
}

// toggleHelp flips the shortcuts panel on/off, resizing the viewport so the panel
// does not push the chrome off-screen.
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
	m.activate(m.order[idx])
}

// activate focuses the tab for ch: it clears its unread marker and snaps the
// viewport to its latest output. A no-op if ch is already active or unknown, so
// re-clicking the current tab does not fight an in-progress scroll.
func (m *model) activate(ch string) {
	if ch == m.active || m.tabs[ch] == nil {
		return
	}
	m.active = ch
	m.tabs[ch].unread = false
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
		tb.appendChunk(e.Text)
	case "status":
		tb.busy = true
		tb.endStream() // a tool line ends the current prose block
		tb.appendEntry(entry{role: roleStatus, text: e.Text})
	case "reply":
		// A streamed answer is already on screen from its chunks; rendering the
		// final reply text again is the duplicate we are killing. The streamed
		// flag holds whether or not the reply repeats the text, so a
		// non-streaming backend still renders reply.Text exactly once.
		if e.Text != "" && !tb.streamed {
			tb.appendEntry(entry{role: roleAgent, text: e.Text})
		}
		tb.endStream()
		if e.Cost > 0 {
			tb.lastCost = e.Cost
			tb.appendEntry(entry{role: roleCost, text: formatCost(e.Cost)})
		}
		if e.Done {
			tb.busy = false
			tb.streamed = false
		}
	case "reset":
		tb.busy = false
		tb.streamed = false
		tb.endStream()
		tb.appendEntry(entry{role: roleStatus, text: "(turn reset)"})
	case "abandoned":
		tb.busy = false
		tb.streamed = false
		tb.disconnected = true
		tb.endStream()
		tb.appendEntry(entry{role: roleStatus, text: "(turn abandoned)"})
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
	content := m.cachedTranscript(tb)
	if tb.busy && !tb.streamed {
		line := spinnerStyle.Render(m.spinFrame() + " thinking…")
		if content != "" {
			content += "\n" + line
		} else {
			content = line
		}
	}
	return content
}

// cachedTranscript returns the active tab's wrapped transcript, re-wrapping only
// when the cache key changed since the last render. The heavy work (lipgloss
// word-wrap over every entry) thus runs on a real content/width change, not on
// every animation frame.
func (m *model) cachedTranscript(tb *tab) string {
	width := m.vp.Width
	lastLen, lastStr := 0, false
	if n := len(tb.entries); n > 0 {
		lastLen = len(tb.entries[n-1].text)
		lastStr = tb.entries[n-1].streaming
	}
	c := &m.tsCache
	if c.valid && c.ch == tb.channel && c.width == width &&
		c.entries == len(tb.entries) && c.lastLen == lastLen && c.lastStr == lastStr {
		return c.out
	}
	out := m.renderTranscript(tb, width)
	*c = transcriptCache{
		ch: tb.channel, width: width, entries: len(tb.entries),
		lastLen: lastLen, lastStr: lastStr, out: out, valid: true,
	}
	return out
}

// inputRow renders the multi-line composer. The key hint lives on the status row
// above it (see statusRow), since a growing composer can no longer share its line.
func (m *model) inputRow() string {
	return m.input.View()
}

// hintText is the dim key cheatsheet under the composer, switching to menu
// navigation keys while an inline menu is open.
func (m *model) hintText() string {
	if m.paletteOpen() {
		return dimStyle.Render("↑↓ navigate · Tab complete · Esc close")
	}
	return dimStyle.Render("/ cmds · @ files · ? shortcuts · ⌥⏎ newline · esc interrupt")
}

// statusRow is the footer status on the left and the key hint on the right,
// separated to fill the width. left is already styled (footer or flash).
func (m *model) statusRow(left string) string {
	hint := m.hintText()
	gap := m.innerWidth() - lipgloss.Width(left) - lipgloss.Width(hint)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + hint
}

// footer renders the spinner/status line for the active tab: a warm spinner hint
// while busy, a dim disconnected/cost note otherwise.
func (m *model) footer() string {
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	if tb.busy {
		return spinnerStyle.Render(m.spinnerHint(tb))
	}
	if tb.disconnected {
		return dimStyle.Render("· disconnected")
	}
	if tb.lastCost > 0 {
		return dimStyle.Render("· " + formatCost(tb.lastCost))
	}
	return ""
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
	// WithInput strips the terminal's in-band colour/cursor query responses before
	// they reach the key parser (see filteredStdin); WithMouseCellMotion enables
	// click-to-focus on the tab strip. filteredStdin embeds os.Stdin, so raw mode
	// and TTY detection still apply to the real terminal.
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithInput(newFilteredStdin()),
	)
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
	return tea.Batch(textarea.Blink, tickCmd())
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
		m.input.SetWidth(m.innerWidth())
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
		// The permission menu is modal and takes precedence: arrows move the
		// selection, Enter submits the chosen option as a pick, Esc denies (selects
		// the last, safe option); every other key is swallowed.
		if m.choice != nil {
			switch msg.Type {
			case tea.KeyUp:
				m.moveChoice(-1)
				return m, nil
			case tea.KeyDown:
				m.moveChoice(1)
				return m, nil
			case tea.KeyEsc:
				m.choiceIdx = len(m.choice.Options) - 1 // esc = the final deny row
				m.chooseChoice()
				return m, nil
			case tea.KeyEnter:
				m.chooseChoice()
				return m, nil
			case tea.KeyCtrlC:
				return m, tea.Quit
			}
			return m, nil
		}
		// The /session switch picker is modal: arrows move the selection, Enter
		// focuses the chosen session, Esc closes it; every other key is swallowed.
		if m.switchOpen {
			switch msg.Type {
			case tea.KeyUp:
				m.moveSwitch(-1)
				return m, nil
			case tea.KeyDown:
				m.moveSwitch(1)
				return m, nil
			case tea.KeyEsc:
				m.switchOpen = false
				m.applySize()
				m.syncViewport()
				return m, nil
			case tea.KeyEnter:
				m.chooseSwitch()
				m.switchOpen = false
				m.applySize()
				m.syncViewport()
				return m, m.ensureSpin()
			case tea.KeyCtrlC:
				return m, tea.Quit
			}
			return m, nil
		}
		// The /resume picker is modal: arrows move the selection, Enter revives or
		// focuses the chosen session, Esc closes it; every other key is swallowed.
		if m.resumeOpen {
			switch msg.Type {
			case tea.KeyUp:
				m.moveResume(-1)
				return m, nil
			case tea.KeyDown:
				m.moveResume(1)
				return m, nil
			case tea.KeyEsc:
				m.resumeOpen = false
				m.applySize()
				m.syncViewport()
				return m, nil
			case tea.KeyEnter:
				cmd := m.chooseResume()
				m.resumeOpen = false
				m.applySize()
				m.syncViewport()
				return m, tea.Batch(cmd, m.ensureSpin())
			case tea.KeyCtrlC:
				return m, tea.Quit
			}
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
		// The inline @ file-mention list, when the cursor word is an @-mention with
		// matches: arrows move the selection, Tab inserts the selected path as plain
		// text; other keys fall through to keep editing the word.
		if m.mentionOpen() {
			switch msg.Type {
			case tea.KeyUp:
				m.moveMention(-1)
				return m, nil
			case tea.KeyDown:
				m.moveMention(1)
				return m, nil
			case tea.KeyTab:
				m.completeActiveMention()
				m.applySize()
				m.syncViewport()
				return m, nil
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
			if msg.Alt {
				break // Alt+Enter falls through to the composer as a newline
			}
			return m, tea.Batch(m.handleEnter(), m.ensureSpin())
		case tea.KeyCtrlV:
			// A clipboard image is staged as an attachment; anything else falls
			// through to the composer so Ctrl+V still pastes text.
			if m.pasteImage() {
				return m, nil
			}
		case tea.KeyCtrlU:
			// Unstage the last attachment; with none staged, fall through so the
			// composer keeps its delete-to-line-start behaviour.
			if m.removeLastPending() {
				m.applySize() // the chip row may have just disappeared
				m.syncViewport()
				return m, nil
			}
		case tea.KeyUp:
			// On an empty or already-recalling single-line composer, ↑ walks back
			// through submitted prompts; otherwise it falls through to the composer.
			if m.input.LineCount() <= 1 && (m.input.Value() == "" || m.histIdx < len(m.history)) {
				if m.recallHistory(-1) {
					return m, nil
				}
			}
		case tea.KeyDown:
			if m.input.LineCount() <= 1 && m.histIdx < len(m.history) {
				if m.recallHistory(1) {
					return m, nil
				}
			}
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
				tb.appendEntry(entry{role: roleStatus, text: line})
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
	if _, ok := msg.(tea.KeyMsg); ok {
		m.resizeComposer() // an edit may have added/removed a composer row
	}
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

// helpView returns the one-line dim shortcuts panel toggled by ? (and /help).
func (m *model) helpView() string {
	return dimStyle.Render("↑↓ history/scroll · ⏎ send · ⌥⏎ newline · esc interrupt · ctrl+v paste image · / commands · @ files")
}

func (m *model) View() string {
	if !m.ready {
		return "starting…"
	}
	footer := m.footer()
	if m.flash != "" {
		footer = dimStyle.Render("· " + m.flash)
	}
	footer = m.statusRow(footer)
	// Full-width Claude flow: transcript → inline menu → status/spinner → input →
	// hint. No enclosing card, no brand row, no permanent tab strip.
	parts := []string{m.vp.View()}
	if m.choice != nil {
		parts = append(parts, m.choiceView())
	}
	if m.paletteOpen() {
		parts = append(parts, m.paletteView())
	}
	if m.mentionOpen() {
		parts = append(parts, m.mentionView())
	}
	if m.resumeOpen {
		parts = append(parts, m.resumeView())
	}
	if m.switchOpen {
		parts = append(parts, m.switchView())
	}
	if m.showHelp {
		parts = append(parts, m.helpView())
	}
	if chips := chipRow(m.pending); chips != "" {
		parts = append(parts, chips+"  "+dimStyle.Render("⌃U remove"))
	}
	parts = append(parts, footer, m.inputRow())
	return strings.Join(parts, "\n")
}
