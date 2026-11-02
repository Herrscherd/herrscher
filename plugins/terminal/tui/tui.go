// Package tui renders a gateway's live event stream and captures the operator's
// input, driving it through the narrow Backend interface. It is the terminal
// gateway's frontend: the daemon hub treats that gateway like any other; the TUI
// is what makes it a human-usable pane. Depending on Backend (not the concrete
// gateway) keeps this package importable by the gateway without a cycle.
//
// Rendering is split by concern: tui.go maps bus events onto transcript roles,
// render.go draws one entry per role, markdown.go runs the agent's prose through
// the markdown engine (and its diffs by hand), chrome.go frames the flow, and
// status.go derives what the session has cost and how full its context is. The
// palette lives in theme.go, so no render site names a colour.
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

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/skills"
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
	// Interrupt cancels a session's in-flight turn (esc-to-interrupt). Reports
	// false when nothing was interrupted (no live turn by that name).
	Interrupt(name string) bool
}

const (
	roleYou        = "you"
	roleAgent      = "agent"
	roleThinking   = "thinking"
	roleTool       = "tool"
	roleNotice     = "notice"
	roleError      = "error"
	roleCost       = "cost"
	roleScrollback = "scrollback"
)

const hostErrPrefix = "⚠️ "

func hostError(text string) (string, bool) {
	trimmed := strings.TrimSpace(text)
	if rest, ok := strings.CutPrefix(trimmed, hostErrPrefix); ok {
		return strings.TrimSpace(rest), true
	}
	return text, false
}

func statusRole(text string) string {
	if _, isErr := hostError(text); isErr {
		return roleError
	}
	return roleTool
}

type entry struct {
	id          uint64
	role        string
	text        string
	streaming   bool
	attachments []Attachment
	preview     string
}

type tab struct {
	channel      string
	label        string
	entries      []entry
	unread       bool
	busy         bool
	streamed     bool
	lastCost     float64
	disconnected bool
	tokens       int
	startedAt    time.Time
	costTotal    float64
	ctxTokens    int
	ctxMeasured  bool
	openedAt     time.Time
	nextEntryID  uint64
	todos        []contracts.TodoItem
	agents       []liveAgent
}

type liveAgent struct {
	contracts.Subagent
	since time.Time
}

func (tb *tab) dropAgent(id string) {
	for i, a := range tb.agents {
		if a.ID == id {
			tb.agents = append(tb.agents[:i], tb.agents[i+1:]...)
			return
		}
	}
}

func (tb *tab) clearPanels() {
	tb.agents = nil
	for _, t := range tb.todos {
		if t.State != "done" {
			return
		}
	}
	tb.todos = nil
}

const maxTabLines = 5000

func (tb *tab) appendEntry(e entry) {
	tb.nextEntryID++
	e.id = tb.nextEntryID
	tb.entries = append(tb.entries, e)
	if len(tb.entries) > maxTabLines {
		tb.entries = append([]entry(nil), tb.entries[len(tb.entries)-maxTabLines:]...)
	}
}

func (tb *tab) appendChunk(text string) {
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent && tb.entries[n-1].streaming {
		tb.entries[n-1].text += text
		return
	}
	tb.appendEntry(entry{role: roleAgent, text: text, streaming: true})
}

func (tb *tab) endStream() {
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent {
		tb.entries[n-1].streaming = false
	}
}

type dispatchResultMsg struct {
	origin string
	out    string
	err    error
}

type eventMsg RoutedEvent

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

type spinMsg struct{}

const fastTick = 200 * time.Millisecond

func spinTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return spinMsg{} })
}

type model struct {
	ctx          context.Context
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
	flash        string
	tabSig       string
	spin         int
	cmds         []CommandSpec
	palIdx       int
	palWasOpen   bool
	spinning     bool
	composerRows int

	infoCache   contracts.SessionInfo
	infoKnown   bool
	infoChannel string
	infoAt      time.Time

	openSession string
	openText    string

	resumeOpen bool
	resumeIdx  int
	resumeRows []contracts.SessionInfo

	switchOpen bool
	switchIdx  int
	switchRows []contracts.SessionInfo

	diagOpen bool

	skillsOpen bool
	skillsIdx  int
	skillsRows []skills.Skill

	pluginsOpen     bool
	pluginsSeam     PluginSeam
	pluginsMode     pluginsMode
	pluginsIdx      int
	pluginsRows     []PluginRow
	pluginsMenu     []string
	pluginsMenuIdx  int
	pluginsReq      pluginsRequest
	pluginsFindings []string
	pluginsOutput   string
	pluginsNotice   string

	choice    *PendingChoice
	choiceIdx int

	mentionIdx       int
	mentionCacheKey  string
	mentionCacheRows []string

	history []string
	histIdx int

	clip      clipboard
	pending   []Attachment
	attachSeq int

	mouseFree bool

	caps Capabilities

	imageFetcher func(context.Context, string) ([]byte, error)
	imageHosts   []string

	links   []Link
	linkIdx int

	sys  launcher
	edit launcher

	searchOpen   bool
	searchQuery  string
	searchHits   []int
	searchIdx    int
	searchReturn int

	foldTurnsOn bool

	foldCode bool

	expandTools bool

	tsCache transcriptCache

	foldCache overlayCache
	markCache overlayCache
}

type overlayCache struct {
	in  string
	arg string
	out string
}

func (c *overlayCache) get(in, arg string, apply func(string) string) string {
	if c.out != "" && c.in == in && c.arg == arg {
		return c.out
	}
	out := apply(in)
	*c = overlayCache{in: in, arg: arg, out: out}
	return out
}

type transcriptCache struct {
	ch      string
	width   int
	entries int
	lastLen int
	lastStr bool
	out     string
	valid   bool
}

const maxComposerLines = 8

func (m *model) composerHeight() int {
	if m.composerRows >= 1 {
		return m.composerRows
	}
	return 1
}

func (m *model) resizeComposer() {
	h := displayRows(m.input.Value(), m.input.Width())
	if h < 1 {
		h = 1
	}
	if h > maxComposerLines {
		h = maxComposerLines
	}
	if h != m.composerRows {
		m.composerRows = h
		m.applySize()
		m.syncViewport()
	}
}

func (m *model) chromeHeight() int {
	return m.buildFrame().height()
}

func (m *model) innerWidth() int {
	if m.width < 1 {
		return 1
	}
	return m.width
}

func (m *model) applySize() {
	if !m.ready {
		return
	}
	m.vp.Width = m.innerWidth()
	m.vp.Height = m.height - m.chromeHeight()
}

func (m *model) runContext() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func newModel(tm Backend) *model {
	in := textarea.New()
	in.Placeholder = "type a message…"
	in.Prompt = ""
	in.ShowLineNumbers = false
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "shift+enter", "ctrl+j"))
	in.SetHeight(maxComposerLines)
	in.Focus()
	m := &model{
		ctx: context.Background(), tm: tm, input: in, composerRows: 1, tabs: map[string]*tab{}, clip: newClipboard(), caps: Probe(os.Getenv), linkIdx: -1,
		sys: systemLauncher(), edit: editorLauncher(os.Getenv),
	}
	m.cmds = localCommands()
	if tm != nil {
		m.cmds = mergeCommands(m.cmds, tm.Commands())
	}
	return m
}

func (m *model) ensureTab(channel string) *tab {
	if tb, ok := m.tabs[channel]; ok {
		return tb
	}
	tb := &tab{channel: channel, label: channel, openedAt: time.Now()}
	m.tabs[channel] = tb
	m.order = append(m.order, channel)
	if m.active == "" {
		m.active = channel
	}
	m.seedScrollback(tb)
	return tb
}

func (m *model) seedScrollback(tb *tab) {
	if m.tm == nil {
		return
	}
	name := m.sessionName(tb.channel)
	if name == "" {
		return
	}
	lines := m.tm.Scrollback(name)
	if len(lines) == 0 {
		return
	}
	for _, ln := range lines {
		tb.appendEntry(entry{role: scrollbackRole(ln.Role), text: ln.Text})
	}
	tb.appendEntry(entry{role: roleScrollback, text: "earlier"})
}

func scrollbackRole(role string) string {
	if role == "user" {
		return roleYou
	}
	return roleAgent
}

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

func (m *model) route(re RoutedEvent) tea.Cmd {
	if re.Event.T == "closed" {
		m.removeTab(re.Conv.ID)
		return nil
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
	if re.Event.T != "reply" || len(tb.entries) == 0 {
		return nil
	}
	last := len(tb.entries) - 1
	for last >= 0 && tb.entries[last].role != roleAgent {
		last--
	}
	if last < 0 {
		return nil
	}
	return m.fetchEntryImages(tb, last)
}

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
		return
	}
	m.tabSig = sig
	live := map[string]bool{}
	for _, s := range infos {
		live[s.ChannelID] = true
		if s.Archived {
			continue
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
	m.focusOpen(infos)
}

func (m *model) focusOpen(infos []contracts.SessionInfo) {
	if m.openSession == "" {
		return
	}
	channel := ""
	for _, s := range infos {
		if s.Name == m.openSession {
			channel = s.ChannelID
			break
		}
	}
	if channel == "" || m.tabs[channel] == nil {
		return
	}
	text := m.openText
	m.openSession, m.openText = "", ""
	m.active = channel
	if text != "" {
		m.submitTo(channel, text, nil)
	}
}

func (m *model) continueLine() bool {
	val := m.input.Value()
	if !strings.HasSuffix(val, `\`) || strings.HasSuffix(val, `\\`) {
		return false
	}
	m.input.SetValue(strings.TrimSuffix(val, `\`) + "\n")
	m.resizeComposer()
	return true
}

func (m *model) handleEnter() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" && len(m.pending) == 0 {
		return nil
	}
	m.input.Reset()
	m.resizeComposer()
	if strings.HasPrefix(text, "/") {
		args := strings.Fields(strings.TrimPrefix(text, "/"))
		if len(args) == 0 {
			return nil
		}
		if args[0] == "resume" {
			m.openResume()
			return nil
		}
		if args[0] == "clear" {
			m.clearActive()
			return nil
		}
		if args[0] == "help" {
			m.toggleHelp()
			return nil
		}
		if args[0] == "session" && len(args) >= 2 && args[1] == "switch" {
			m.openSwitch()
			return nil
		}
		if args[0] == "usage" {
			if tb := m.tabs[m.active]; tb != nil {
				tb.appendEntry(entry{role: roleNotice, text: m.usageReport(tb)})
				m.syncViewport()
			}
			return nil
		}
		if args[0] == "capabilities" {
			m.openDiagnostics()
			return nil
		}
		if args[0] == "skills" {
			m.openSkills()
			return nil
		}
		if args[0] == "plugins" {
			return m.openPlugins()
		}
		if args[0] == "attach" {
			m.stageAttachment(strings.TrimSpace(strings.TrimPrefix(text, "/attach")))
			return nil
		}
		if args[0] == "session" && len(args) >= 2 && args[1] == "close" {
			return m.closeCmd(args[2:])
		}
		if args[0] == "rename" {
			return m.renameCmd(args[1:])
		}
		if args[0] == "copy" {
			m.copyCmd(args[1:])
			return nil
		}
		if args[0] == "mouse" {
			return m.toggleMouse()
		}
		return m.dispatchCmd(m.active, args)
	}
	if m.active == "" {
		return nil
	}
	atts := m.pending
	m.pending = nil
	m.applySize()
	m.recordHistory(text)
	m.submitTo(m.active, text, atts)
	return nil
}

func (m *model) submitTo(channel, text string, atts []Attachment) {
	tb := m.tabs[channel]
	if tb == nil {
		return
	}
	m.tm.Submit(channel, text, atts)
	tb.endStream()
	e := entry{role: roleYou, text: text, attachments: atts}
	e.preview = previewEscapes(atts, m.caps)
	tb.appendEntry(e)
	tb.busy = true
	tb.streamed = false
	tb.tokens = 0
	tb.startedAt = time.Now()
	m.syncViewport()
}

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
		m.flash = "collage impossible : " + err.Error()
		return true
	}
	att, err := saveClipboardImage(data, mime, m.attachSeq)
	if err != nil {
		m.flash = err.Error()
		return true
	}
	m.attachSeq++
	m.pending = append(m.pending, att)
	m.applySize()
	m.syncViewport()
	return true
}

func (m *model) stageAttachment(path string) {
	att, err := attachLocalFile(path, m.attachSeq)
	if err != nil {
		m.flash = err.Error()
		return
	}
	m.attachSeq++
	m.pending = append(m.pending, att)
	m.applySize()
	m.syncViewport()
}

func (m *model) removeLastPending() bool {
	if len(m.pending) == 0 {
		return false
	}
	m.pending = m.pending[:len(m.pending)-1]
	return true
}

func (m *model) dispatchCmd(origin string, args []string) tea.Cmd {
	tm := m.tm
	return func() tea.Msg {
		out, err := tm.Dispatch(args)
		return dispatchResultMsg{origin: origin, out: out, err: err}
	}
}

func (m *model) recordHistory(text string) {
	if text == "" {
		return
	}
	if n := len(m.history); n == 0 || m.history[n-1] != text {
		m.history = append(m.history, text)
	}
	m.histIdx = len(m.history)
}

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

func (m *model) toggleHelp() {
	m.showHelp = !m.showHelp
	m.applySize()
	m.syncViewport()
}

func (m *model) sessionName(channel string) string {
	for _, s := range m.tm.Sessions() {
		if s.ChannelID == channel && s.Name != "" {
			return s.Name
		}
	}
	return ""
}

func (m *model) activeSessionName() string {
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	if name := m.sessionName(tb.channel); name != "" {
		return name
	}
	return tb.label
}

func (m *model) confirmClose() tea.Cmd {
	name := m.activeSessionName()
	if name == "" {
		return nil
	}
	return m.closeSession(name, false)
}

func (m *model) closeSession(name string, force bool) tea.Cmd {
	origin, tm := m.active, m.tm
	return func() tea.Msg {
		out, err := tm.Close(name, force)
		return dispatchResultMsg{origin: origin, out: out, err: err}
	}
}

func (m *model) closeCmd(rest []string) tea.Cmd {
	name, force := closeTarget(rest)
	if name == "" {
		name = m.activeSessionName()
	}
	if name == "" {
		m.flash = "aucune session à fermer ici"
		return nil
	}
	return m.closeSession(name, force)
}

func (m *model) renameCmd(rest []string) tea.Cmd {
	to := renameTarget(rest)
	if to == "" {
		m.flash = "/rename <nom> : donne le nouveau nom de la session"
		return nil
	}
	name := m.activeSessionName()
	if name == "" {
		m.flash = "aucune session à renommer ici"
		return nil
	}
	return m.dispatchCmd(m.active, []string{"session", "rename", "--name", name, "--to", to})
}

func renameTarget(rest []string) string {
	var words []string
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		switch {
		case tok == "--to":
			if i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "--") {
				words = append(words, rest[i+1])
				i++
			}
		case strings.HasPrefix(tok, "--to="):
			words = append(words, strings.TrimPrefix(tok, "--to="))
		case !strings.HasPrefix(tok, "--"):
			words = append(words, tok)
		}
	}
	return strings.Join(words, " ")
}

func closeTarget(rest []string) (name string, force bool) {
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		switch {
		case tok == "--force":
			force = true
		case tok == "--name":
			if i+1 < len(rest) && !strings.HasPrefix(rest[i+1], "--") {
				name = rest[i+1]
				i++
			}
		case strings.HasPrefix(tok, "--name="):
			name = strings.TrimPrefix(tok, "--name=")
		case !strings.HasPrefix(tok, "--") && name == "":
			name = tok
		}
	}
	return name, force
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

func (m *model) activate(ch string) {
	if ch == m.active || m.tabs[ch] == nil {
		return
	}
	m.active = ch
	m.tabs[ch].unread = false
	m.syncViewport()
	m.vp.GotoBottom()
}

func (m *model) renderInto(tb *tab, e contracts.Event) {
	if e.T != "abandoned" {
		tb.disconnected = false
	}
	if e.Tokens > 0 {
		tb.tokens = e.Tokens
	}
	if used := e.TokensIn + e.CacheRead + e.CacheCreate; used > 0 {
		if !e.Done {
			tb.ctxTokens, tb.ctxMeasured = used, true
		} else if !tb.ctxMeasured {
			tb.ctxTokens = used
		}
	}
	switch e.T {
	case "chunk":
		tb.busy = true
		tb.streamed = true
		tb.appendChunk(e.Text)
	case "thinking":
		tb.busy = true
		if strings.TrimSpace(e.Text) == "" {
			break
		}
		tb.endStream()
		tb.appendEntry(entry{role: roleThinking, text: e.Text})
	case "status":
		tb.busy = true
		tb.endStream()
		text, _ := hostError(e.Text)
		tb.appendEntry(entry{role: statusRole(e.Text), text: text})
	case "reply":
		if e.Text != "" && !tb.streamed {
			if text, isErr := hostError(e.Text); isErr {
				tb.appendEntry(entry{role: roleError, text: text})
			} else {
				tb.appendEntry(entry{role: roleAgent, text: e.Text})
			}
		}
		tb.endStream()
		if e.Cost > 0 {
			tb.lastCost = e.Cost
			tb.costTotal += e.Cost
			tb.appendEntry(entry{role: roleCost, text: formatCost(e.Cost)})
		}
		if e.Done {
			tb.busy = false
			tb.streamed = false
			tb.ctxMeasured = false
			tb.clearPanels()
		}
	case "todos":
		tb.busy = true
		tb.todos = e.Todos
	case "subagent":
		tb.busy = true
		if e.Subagent == nil {
			break
		}
		if e.Subagent.State == "done" {
			tb.dropAgent(e.Subagent.ID)
			break
		}
		tb.agents = append(tb.agents, liveAgent{Subagent: *e.Subagent, since: time.Now()})
	case "reset":
		tb.busy = false
		tb.streamed = false
		tb.ctxMeasured = false
		tb.endStream()
		tb.appendEntry(entry{role: roleNotice, text: "tour réinitialisé"})
	case "abandoned":
		tb.busy = false
		tb.streamed = false
		tb.ctxMeasured = false
		tb.disconnected = true
		tb.endStream()
		tb.appendEntry(entry{role: roleNotice, text: "tour abandonné"})
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
	atBottom := m.vp.AtBottom()
	m.vp.SetContent(m.thinkingContent())
	if atBottom {
		m.vp.GotoBottom()
	}
}

func (m *model) anyBusy() bool {
	for _, tb := range m.tabs {
		if tb.busy {
			return true
		}
	}
	return false
}

func (m *model) spinFrame() string { return spinFrames[m.spin%len(spinFrames)] }

func (m *model) thinkingContent() string {
	return m.contentWith(m.searchQuery)
}

func (m *model) baseContent() string { return m.contentWith("") }

func (m *model) contentWith(query string) string {
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	if len(tb.entries) == 0 && !tb.busy {
		return m.emptyState(m.vp.Width)
	}
	content := m.cachedTranscript(tb)
	if m.foldTurnsOn {
		content = m.foldCache.get(content, "", func(s string) string {
			return strings.Join(foldTurns(strings.Split(s, "\n"), turnFoldKeep), "\n")
		})
	}
	if m.searchOpen && strings.TrimSpace(query) != "" {
		content = m.markCache.get(content, query, func(s string) string {
			return strings.Join(highlightMatches(strings.Split(s, "\n"), query), "\n")
		})
	}
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

func (m *model) inputRow() string {
	lines := strings.Split(m.input.View(), "\n")
	if h := m.composerHeight(); h < len(lines) {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (m *model) hintText() string {
	if m.paletteOpen() {
		return dimStyle.Render("↑↓ naviguer · ⇞⇟ page · Tab compléter · Échap fermer")
	}
	return dimStyle.Render("? raccourcis")
}

func (m *model) footer() string {
	if s := m.linkStatus(); s != "" {
		return s
	}
	tb := m.tabs[m.active]
	if tb == nil {
		return ""
	}
	if tb.busy {
		return spinnerStyle.Render(m.spinnerHint(tb))
	}
	if tb.disconnected {
		return dimStyle.Render("· déconnecté")
	}
	return m.statusBar(tb, m.innerWidth())
}

func (m *model) spinnerHint(tb *tab) string {
	segs := []string{"échap pour interrompre"}
	if !tb.startedAt.IsZero() {
		segs = append(segs, fmt.Sprintf("%ds", int(time.Since(tb.startedAt).Seconds())))
	}
	if tb.tokens > 0 {
		segs = append(segs, "↑ "+formatTokens(tb.tokens))
	}
	if tb.lastCost > 0 {
		segs = append(segs, formatCost(tb.lastCost))
	}
	return m.spinFrame() + " " + workingVerb + " (" + strings.Join(segs, " · ") + ")"
}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func formatCost(c float64) string {
	if c < 0.01 {
		return fmt.Sprintf("$%.4f", c)
	}
	return fmt.Sprintf("$%.2f", c)
}

// Option adjusts a window before it is drawn. Options exist so a caller can say
// what the window opens on without the TUI growing a constructor per launch path.
type Option func(*model)

// OpenOn opens the window on a named session and sends text there as its first
// message. An empty text opens the session and sends nothing, which is how a
// caller says "show me this one" without starting a turn.
func OpenOn(session, text string) Option {
	return func(m *model) { m.openSession, m.openText = session, text }
}

// Run starts the TUI bound to the given gateway backend, blocking until the user
// quits; quitting cancels ctx (wired by the caller) so the daemon shuts down
// cleanly.
func Run(ctx context.Context, cancel context.CancelFunc, tm Backend, opts ...Option) error {
	m := newModel(tm)
	m.ctx = ctx
	for _, opt := range opts {
		opt(m)
	}
	if seam, err := newManageSeam(); err == nil {
		m.SetPluginSeam(seam)
	}
	os.Stdout.WriteString(enhancedKeysOn)
	defer os.Stdout.WriteString(enhancedKeysOff)
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
	return tea.Batch(textarea.Blink, tickCmd(), enableEnhancedKeys)
}

func (m *model) ensureSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return spinTick(fastTick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if cmd, handled := m.handlePluginsMsg(msg); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if !m.ready {
			m.vp = viewport.New(msg.Width, msg.Height-m.chromeHeight())
			m.ready = true
		} else {
			m.applySize()
		}
		m.input.SetWidth(m.composerTextWidth())
		m.resizeComposer()
		m.syncViewport()
	case tea.KeyMsg:
		keylog("key type=%d alt=%v str=%q", msg.Type, msg.Alt, msg.String())
		m.flash = ""
		if m.pendingClose {
			if msg.String() == "y" {
				cmd := m.confirmClose()
				m.pendingClose = false
				return m, cmd
			}
			if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
				m.pendingClose = false
				return m, tea.Quit
			}
			m.pendingClose = false
			return m, nil
		}
		if m.choice != nil {
			switch msg.Type {
			case tea.KeyUp:
				m.moveChoice(-1)
				return m, nil
			case tea.KeyDown:
				m.moveChoice(1)
				return m, nil
			case tea.KeyEsc:
				m.choiceIdx = len(m.choice.Options) - 1
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
		if m.switchOpen {
			if m.overlayNav(msg, m.moveSwitch) {
				return m, nil
			}
			switch msg.Type {
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
		if m.searchOpen {
			switch msg.Type {
			case tea.KeyCtrlC:
				return m, tea.Quit
			case tea.KeyEsc, tea.KeyCtrlS:
				m.closeSearch()
				return m, nil
			case tea.KeyEnter, tea.KeyCtrlN:
				m.searchStep(1)
				return m, nil
			case tea.KeyCtrlP:
				m.searchStep(-1)
				return m, nil
			case tea.KeyBackspace:
				if q := m.searchQuery; q != "" {
					m.typeSearch(q[:len(q)-1])
				}
				return m, nil
			case tea.KeyRunes:
				if msg.Alt {
					return m, nil
				}
				m.typeSearch(m.searchQuery + string(msg.Runes))
				return m, nil
			case tea.KeySpace:
				m.typeSearch(m.searchQuery + " ")
				return m, nil
			}
			return m, nil
		}
		if m.diagOpen {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			m.diagOpen = false
			m.applySize()
			m.syncViewport()
			return m, nil
		}
		if m.pluginsOpen {
			if msg.Type == tea.KeyCtrlC {
				return m, tea.Quit
			}
			return m, m.handlePluginsKey(msg)
		}
		if m.skillsOpen {
			if m.overlayNav(msg, m.moveSkills) {
				return m, nil
			}
			switch msg.Type {
			case tea.KeyEsc, tea.KeyEnter:
				m.skillsOpen = false
				m.applySize()
				m.syncViewport()
				return m, nil
			case tea.KeyCtrlC:
				return m, tea.Quit
			}
			return m, nil
		}
		if m.resumeOpen {
			if m.overlayNav(msg, m.moveResume) {
				return m, nil
			}
			switch msg.Type {
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
		if m.paletteOpen() {
			if m.overlayNav(msg, m.movePal) {
				return m, nil
			}
			switch msg.Type {
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
		case tea.KeyEsc:
			if tb := m.tabs[m.active]; tb != nil && tb.busy {
				if name := m.sessionName(m.active); name != "" {
					m.tm.Interrupt(name)
				}
				return m, nil
			}
			return m, tea.Quit
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyTab:
			m.switchTab(1)
			return m, nil
		case tea.KeyShiftTab:
			m.switchTab(-1)
			return m, nil
		case tea.KeyEnter:
			if msg.Alt {
				break
			}
			if m.continueLine() {
				return m, nil
			}
			return m, tea.Batch(m.handleEnter(), m.ensureSpin())
		case tea.KeyCtrlS:
			m.openSearch()
			return m, nil
		case tea.KeyCtrlT:
			m.toggleTurnFold()
			return m, nil
		case tea.KeyCtrlF:
			m.toggleFold()
			return m, nil
		case tea.KeyCtrlY:
			m.copyLastCode()
			return m, nil
		case tea.KeyCtrlG:
			return m, m.toggleMouse()
		case tea.KeyCtrlL:
			m.selectNextLink()
			return m, nil
		case tea.KeyCtrlO:
			m.openSelectedLink()
			return m, nil
		case tea.KeyCtrlV:
			if m.pasteImage() {
				return m, nil
			}
		case tea.KeyCtrlU:
			if m.removeLastPending() {
				m.applySize()
				m.syncViewport()
				return m, nil
			}
		case tea.KeyUp:
			if msg.Alt {
				m.jumpTurn(-1)
				return m, nil
			}
			if m.input.LineCount() <= 1 && (m.input.Value() == "" || m.histIdx < len(m.history)) {
				if m.recallHistory(-1) {
					return m, nil
				}
			}
		case tea.KeyDown:
			if msg.Alt {
				m.jumpTurn(1)
				return m, nil
			}
			if m.input.LineCount() <= 1 && m.histIdx < len(m.history) {
				if m.recallHistory(1) {
					return m, nil
				}
			}
		case tea.KeyCtrlW:
			if m.input.Value() != "" {
				break
			}
			m.pendingClose = true
			return m, nil
		case tea.KeyRunes:
			if msg.String() == "?" && m.input.Value() == "" {
				m.toggleHelp()
				return m, nil
			}
			if msg.Alt && strings.EqualFold(string(msg.Runes), "y") {
				m.copyTarget(copyReply)
				return m, nil
			}
			if msg.Alt && strings.EqualFold(string(msg.Runes), "e") {
				m.toggleToolExpand()
				return m, nil
			}
		}
	case eventMsg:
		cmd := m.route(RoutedEvent(msg))
		return m, tea.Batch(cmd, m.ensureSpin())
	case imageReadyMsg:
		m.applyImage(msg)
		return m, nil
	case dispatchResultMsg:
		m.syncTabs()
		line, role := msg.out, roleNotice
		if msg.err != nil {
			line, role = msg.err.Error(), roleError
		}
		if line != "" {
			tb := m.tabs[msg.origin]
			if tb == nil {
				tb = m.tabs[m.active]
			}
			if tb != nil {
				tb.appendEntry(entry{role: role, text: line})
				m.syncViewport()
			} else {
				m.flash = line
			}
		}
		return m, nil
	case tickMsg:
		m.syncTabs()
		return m, tickCmd()
	case spinMsg:
		if !m.anyBusy() {
			m.spinning = false
			return m, nil
		}
		m.spin++
		m.syncViewport()
		return m, spinTick(fastTick)
	}
	var cmds []tea.Cmd
	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	cmds = append(cmds, c)
	m.vp, c = m.vp.Update(msg)
	cmds = append(cmds, c)
	if _, ok := msg.(tea.KeyMsg); ok {
		m.resizeComposer()
	}
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

func (m *model) helpView() string {
	return dimStyle.Render("⏎ envoyer · ⇧⏎ ou \\⏎ nouvelle ligne · ↑↓ historique · molette/pgup défiler · shift+glisser sélectionner · ctrl+g libérer la souris · échap interrompre · ctrl+v coller une image · ctrl+l lien suivant · ctrl+o l'ouvrir · ctrl+f replier le code · ctrl+y copier le code · alt+y copier la réponse · alt+e déplier commandes et diffs · ctrl+s chercher · ctrl+t replier les tours · alt+↑↓ tour suivant · / commandes · @ fichiers")
}

func (m *model) View() string {
	if !m.ready {
		return "démarrage…"
	}
	return m.buildFrame().render(m.vp.View())
}
