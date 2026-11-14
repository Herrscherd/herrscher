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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/contextwindow"
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
	Pick(channel, value string) bool
	Overview() (string, error)
	Seen(name string)
	StartSession() (string, error)
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

const (
	sessionVerb = "session"
	verbRename  = "rename"
	verbHandoff = "handoff"
	stateDone   = "done"
	stateActive = "active"
	eventReply  = "reply"
	eventReset  = "reset"
	eventStatus = "status"
	eventNotice = "notice"

	stateFailed = "failed"
	stateError  = "error"
)

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
	at          time.Time
	ended       time.Time
	base        int
	tokens      int
	span        time.Duration
	live        bool
	failed      bool
	foldable    bool
	open        bool
	markdown    bool
}

type tab struct {
	channel            string
	label              string
	entries            []entry
	unread             bool
	busy               bool
	interruptRequested bool
	streamed           bool
	lastCost           float64
	disconnected       bool
	tokens             int
	startedAt          time.Time
	costTotal          float64
	ctxTokens          int
	ctxMeasured        bool
	compaction         compaction
	lastCompact        compaction
	openedAt           time.Time
	nextEntryID        uint64
	todos              []contracts.TodoItem
	agents             []liveAgent
	agentsDone         []liveAgent
	background         []backgroundTask
	unreadCount        int
	finished           bool
	files              []string
	trail              []liveAgent
	flips              map[uint64]bool
	pendingApproval    bool
	approval           approvalPrompt
	mode               modeID
	modeAsked          bool
	queue              []queuedInput
	shellContext       string
	shellRunning       bool
	turnStart          string
	snapshotting       bool
	queued             []queuedTurn
}

type liveAgent struct {
	contracts.Subagent

	since time.Time
	ended time.Time
}

func (tb *tab) dropAgent(agentID string) {
	if index := tb.agentIndex(agentID); index >= 0 {
		tb.agents = append(tb.agents[:index], tb.agents[index+1:]...)
	}
}

func (tb *tab) clearPanels() {
	tb.agents = nil
	tb.agentsDone = nil
	for _, t := range tb.todos {
		if t.State != stateDone {
			return
		}
	}
	tb.todos = nil
}

const maxTabLines = 5000

func (tb *tab) appendEntry(item entry) {
	tb.nextEntryID++
	item.id = tb.nextEntryID
	item.at = time.Now()
	if item.role == roleYou {
		tb.trail = nil
	}
	tb.entries = append(tb.entries, item)
	if len(tb.entries) > maxTabLines {
		tb.entries = append([]entry(nil), tb.entries[len(tb.entries)-maxTabLines:]...)
	}
}

func (tb *tab) appendChunk(text string) {
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent && tb.entries[n-1].streaming {
		tb.entries[n-1].text += text
		return
	}
	if strings.TrimSpace(text) == "" {
		return
	}
	tb.appendEntry(entry{role: roleAgent, text: dropBlankLeadingLines(text), streaming: true})
}

func dropBlankLeadingLines(text string) string {
	body := strings.TrimLeft(text, " \t\r\n")
	blank := text[:len(text)-len(body)]
	last := strings.LastIndexByte(blank, '\n')
	if last < 0 {
		return text
	}
	return text[last+1:]
}

func (tb *tab) endStream() {
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleAgent {
		tb.entries[n-1].streaming = false
	}
}

type sessionStartedMsg struct {
	name string
	err  error
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

const fastTick = 60 * time.Millisecond

func spinTick(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return spinMsg{} })
}

type model struct {
	tr               translator
	log              *slog.Logger
	handlers         *dispatchTables
	buildTables      func() (*dispatchTables, error)
	ctx              context.Context
	limits           contextwindow.Limits
	st               uiStyles
	md               *markdownRenderer
	keys             *keyLog
	tm               Backend
	vp               viewport.Model
	input            textarea.Model
	tabs             map[string]*tab
	order            []string
	active           string
	ready            bool
	showHelp         bool
	pendingClose     bool
	width            int
	height           int
	flash            string
	approvalDues     map[string]approvalDue
	approvalRisks    map[string]approvalRating
	approvalSubjects map[string]approvalRating
	tabSig           string
	spin             int
	cmds             []CommandSpec
	diff             diffPanel
	diffRun          commandRunner
	palIdx           int
	palWasOpen       bool
	spinning         bool
	composerRows     int

	infoCache   contracts.SessionInfo
	infoKnown   bool
	infoChannel string
	infoAt      time.Time

	openSession string
	openText    string
	openAtts    []Attachment
	starting    bool

	resumeOpen bool
	resumeIdx  int
	resumeRows []contracts.SessionInfo

	rwOpen bool
	rwStep rewindStep
	rwIdx  int
	rwOpt  rewindOption
	rwRows []rewindRow
	rwWhat rewindWhat
	escAt  time.Time

	switchOpen bool
	switchIdx  int
	switchRows []contracts.SessionInfo

	resumeAll   []contracts.SessionInfo
	resumeQuery string

	surface     surface
	board       boardState
	overviews   []SessionOverview
	summary     panelSummary
	summaryBusy bool
	panes       paneState

	diagOpen bool

	theme           Theme
	themeCat        themeCatalog
	themeCfg        terminalSection
	themeBackground string
	obelFont        fontPresence
	terminal        terminalKind
	themeDir        string
	themeStamp      string
	themeScr        *themeScreen
	noColor         bool

	bindings       *keyTable
	keyChord       keyPress
	keyTimeout     time.Duration
	quitArmedUntil time.Time
	keyProblems    []string
	composerUndo   []string
	bar            barState

	ctxOpen bool
	health  healthPanel
	usage   usagePanel

	toast      toast
	toastSeq   int
	notifyMode notifyMode
	focus      focusState

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

	planMenu *planMenu

	mentionIdx       int
	mentionCacheKey  string
	mentionCacheRows []string

	history          []string
	histIdx          int
	drafts           []string
	histOpen         bool
	histQuery        string
	histSel          int
	editing          bool
	draftFiles       *draftRun
	draftBeatWatched bool
	pendingBeatFlash string
	queueSel         int
	queueFocus       bool
	queueHits        []queueHit

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

	tsCache  transcriptCache
	tsLayout transcriptLayout

	foldCache overlayCache
	markCache overlayCache

	sidebar  sidebarMode
	gitCache map[string]gitCacheEntry
}

type overlayCache struct {
	in  string
	arg string
	out string
}

func (c *overlayCache) get(input, arg string, apply func(string) string) string {
	if c.out != "" && c.in == input && c.arg == arg {
		return c.out
	}
	out := apply(input)
	*c = overlayCache{in: input, arg: arg, out: out}
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
	height := displayRows(m.input.Value(), m.input.Width())
	if height < 1 {
		height = 1
	}
	if height > maxComposerLines {
		height = maxComposerLines
	}
	height = m.fitComposerRows(height)
	if height != m.composerRows {
		m.composerRows = height
		m.applySize()
		m.syncViewport()
	}
}

func (m *model) fitComposerRows(want int) int {
	if m.height <= 0 {
		return want
	}
	saved := m.composerRows
	defer func() { m.composerRows = saved }()
	for rows := want; rows > 1; rows-- {
		m.composerRows = rows
		if m.chromeHeight() <= m.height-minTranscriptRows {
			return rows
		}
	}
	return 1
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
	m.vp.Width = m.transcriptWidth()
	m.vp.Height = max(0, m.height-m.chromeHeight())
}

func (m *model) runContext() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func newModel(backend Backend) *model {
	trans := newTranslator()
	field := textarea.New()
	field.Placeholder = idlePlaceholder(trans)
	field.SetPromptFunc(len(composerPrompt), promptAt(composerPrompt))
	field.ShowLineNumbers = false
	field.SetHeight(maxComposerLines)
	field.Focus()
	match := &model{
		ctx: context.Background(), tr: trans, log: slog.Default(), buildTables: newDispatchTables, tm: backend, input: field, composerRows: 1, tabs: make(map[string]*tab), clip: newClipboard(), caps: Probe(os.Getenv), linkIdx: -1,
		sys: systemLauncher(), edit: editorLauncher(os.Getenv),
		st: newStyles(defaultTheme(), false), md: &markdownRenderer{}, theme: defaultTheme(), bar: newBarState(),
		terminal: detectTerminal(os.LookupEnv),
		bindings: loadKeyBindings(trans, "", "", keysSection{}).table, keyTimeout: defaultChordTimeout,
	}
	match.cmds = localCommands(trans)
	match.syncComposerKeys()
	match.installTables()
	if backend != nil {
		match.cmds = mergeCommands(match.cmds, backend.Commands())
	}
	return match
}

func (m *model) ensureTab(channel string) *tab {
	if tb, ok := m.tabs[channel]; ok {
		return tb
	}
	tab := &tab{channel: channel, label: channel, openedAt: time.Now()}
	m.tabs[channel] = tab
	m.order = append(m.order, channel)
	if m.active == "" {
		m.active = channel
		m.applySize()
	}
	m.seedScrollback(tab)
	return tab
}

func (m *model) seedScrollback(tab *tab) {
	if m.tm == nil {
		return
	}
	name := m.sessionName(tab.channel)
	if name == "" {
		return
	}
	lines := m.tm.Scrollback(name)
	if len(lines) == 0 {
		return
	}
	for _, ln := range lines {
		tab.appendEntry(entry{role: scrollbackRole(ln.Role), text: ln.Text})
	}
	tab.appendEntry(entry{role: roleScrollback, text: m.tr.t("tui.render.earlier", nil)})
}

func scrollbackRole(role string) string {
	if role == "user" {
		return roleYou
	}
	return roleAgent
}

func (m *model) removeTab(channel string) {
	m.removePane(channel)
	if _, ok := m.tabs[channel]; !ok {
		return
	}
	delete(m.tabs, channel)
	out := m.order[:0]
	for _, channelName := range m.order {
		if channelName != channel {
			out = append(out, channelName)
		}
	}
	m.order = out
	if m.active == channel {
		m.active = m.successorTab()
		m.syncViewport()
	}
}

func (m *model) successorTab() string {
	if focused := m.focusedChannel(); m.surface == surfacePanes && m.tabs[focused] != nil {
		return focused
	}
	if len(m.order) > 0 {
		return m.order[0]
	}
	return ""
}

func (m *model) route(routed RoutedEvent) tea.Cmd {
	if routed.Event.T == "closed" {
		m.removeTab(routed.Conv.ID)
		return nil
	}
	tab := m.ensureTab(routed.Conv.ID)
	before := len(tab.entries)
	m.renderInto(tab, routed.Event)
	flushCmd := m.flushQueue(tab, routed.Event.T)
	isReply := routed.Event.T == eventReply
	isTurnDone := isReply && routed.Event.Done
	if tab.channel != m.active {
		if len(tab.entries) != before {
			tab.unread = true
			tab.unreadCount++
		}
		if isTurnDone {
			tab.finished = true
		}
	}
	var gitCmd tea.Cmd
	if tab.channel == m.active {
		m.syncViewport()
		if routed.Event.T == eventStatus {
			gitCmd = m.gitAfterTool(routed.Event.Text)
		}
	}
	var notice tea.Cmd
	if isTurnDone {
		notice = m.notifyTurnEnd(tab)
	}
	var images tea.Cmd
	if isReply && len(tab.entries) > 0 {
		images = m.replyImages(tab)
	}
	return tea.Batch(flushCmd, gitCmd, notice, images)
}

func (m *model) replyImages(tab *tab) tea.Cmd {
	last := lastAgentIndex(tab.entries)
	if last < 0 {
		return nil
	}
	if tab.channel == m.active && tab.modeID() == modePlan && m.planMenu == nil && tab.entries[last].text != "" {
		m.openPlanMenu(tab.channel)
	}
	return m.fetchEntryImages(tab, last)
}

func lastAgentIndex(entries []entry) int {
	last := len(entries) - 1
	for last >= 0 && entries[last].role != roleAgent {
		last--
	}
	return last
}

func (m *model) syncTabs() tea.Cmd {
	infos := m.tm.Sessions()
	if infos == nil {
		return nil
	}
	var builder strings.Builder
	for i := range infos {
		info := &infos[i]
		builder.WriteString(info.ChannelID)
		builder.WriteByte('\x1f')
		builder.WriteString(info.Name)
		builder.WriteByte('\n')
	}
	sig := builder.String()
	if sig == m.tabSig {
		return nil
	}
	m.tabSig = sig
	live := make(map[string]bool)
	for i := range infos {
		session := &infos[i]
		live[session.ChannelID] = true
		if session.Archived {
			continue
		}
		tab := m.ensureTab(session.ChannelID)
		if session.Name != "" {
			tab.label = session.Name
		}
	}
	for _, channel := range append([]string(nil), m.order...) {
		if !live[channel] {
			m.removeTab(channel)
		}
	}
	return m.focusOpen(infos)
}

func (m *model) focusOpen(infos []contracts.SessionInfo) tea.Cmd {
	if m.openSession == "" {
		return nil
	}
	channel := ""
	for i := range infos {
		info := &infos[i]
		if info.Name == m.openSession {
			channel = info.ChannelID
			break
		}
	}
	if channel == "" || m.tabs[channel] == nil {
		return nil
	}
	text, atts := m.openText, m.openAtts
	m.openSession, m.openText, m.openAtts = "", "", nil
	m.active = channel
	m.applySize()
	m.syncViewport()
	if text == "" && len(atts) == 0 {
		return nil
	}
	return m.submitTo(channel, text, atts)
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
	if m.starting {
		m.flash = m.tr.t("tui.session.starting", nil)
		return nil
	}
	m.input.Reset()
	m.resizeComposer()
	if command, ok := shellCommand(text); ok {
		return m.runShell(command)
	}
	if strings.HasPrefix(text, "/") {
		return m.runSlash(text)
	}
	atts := m.pending
	m.pending = nil
	m.applySize()
	m.recordHistory(text)
	if m.tabs[m.active] == nil {
		return m.startSessionWith(text, atts)
	}
	return m.submitTo(m.active, text, atts)
}

func (m *model) startSessionWith(text string, atts []Attachment) tea.Cmd {
	m.starting = true
	m.openText, m.openAtts = text, atts
	m.flash = m.tr.t("tui.session.starting", nil)
	backend := m.tm
	return func() tea.Msg {
		name, err := backend.StartSession()
		return sessionStartedMsg{name: name, err: err}
	}
}

func (m *model) handleSessionStarted(msg sessionStartedMsg) tea.Cmd {
	m.starting = false
	if msg.err != nil {
		m.giveBackFailedStart()
		m.pending = append(m.openAtts, m.pending...)
		m.openText, m.openAtts = "", nil
		m.applySize()
		m.resizeComposer()
		m.flash = m.tr.t("tui.session.start_failed", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
		return nil
	}
	m.flash = ""
	m.openSession = msg.name
	m.tabSig = ""
	return m.syncTabs()
}

func (m *model) giveBackFailedStart() {
	typed := m.input.Value()
	switch {
	case typed == "":
		m.input.SetValue(m.openText)
	case m.openText == "":
	default:
		m.input.SetValue(m.openText + "\n" + typed)
	}
}

func (m *model) runSlash(text string) tea.Cmd {
	args := strings.Fields(strings.TrimPrefix(text, "/"))
	if len(args) == 0 {
		return nil
	}
	if cmd, handled := m.runLocalSlash(text, args); handled {
		return cmd
	}
	return m.dispatchCmd(m.active, args)
}

func (m *model) runLocalSlash(text string, args []string) (tea.Cmd, bool) {
	switch args[0] {
	case "resume":
		m.openResume()
	case "agents":
		return m.openBoard(), true
	case "clear":
		m.clearActive()
	case "help":
		m.toggleHelp()
	case cmdTheme:
		m.openTheme()
	case cmdKeybindings:
		return m.openKeybindings(), true
	case sessionVerb:
		return m.sessionSlash(args)
	case "context":
		m.openContext()
	case "health":
		return m.openHealth(), true
	case "compact":
		return m.compactCmd(), true
	case cmdAccount:
		return m.accountCmd(args[1:]), true
	case "notify":
		return m.cycleNotify(), true
	case "review":
		return m.openDiff(true), true
	case "usage":
		return m.openUsage(), true
	case "capabilities":
		m.openDiagnostics()
	case cmdSkills:
		m.openSkills()
	case "plugins":
		return m.openPlugins(), true
	case "attach":
		m.stageAttachment(strings.TrimSpace(strings.TrimPrefix(text, "/attach")))
	case verbRename:
		return m.renameCmd(args[1:]), true
	case verbHandoff:
		return m.handoffCmd(args[1:]), true
	case "copy":
		m.copyCmd(args[1:])
	case "mouse":
		return m.toggleMouse(), true
	case "statusline":
		return m.openStatusline(), true
	case "mode":
		return m.modeSlash(strings.TrimPrefix(text, "/mode")), true
	case cmdPlan:
		return m.planSlash(strings.TrimPrefix(text, "/plan")), true
	case cmdQueue:
		m.showQueue()
	default:
		return nil, false
	}
	return nil, true
}

const minSessionArgs = 2

func (m *model) sessionSlash(args []string) (tea.Cmd, bool) {
	if len(args) < minSessionArgs {
		return nil, false
	}
	switch args[1] {
	case "switch":
		m.openSwitch()
		return nil, true
	case "close":
		return m.closeCmd(args[2:]), true
	}
	return nil, false
}

func (m *model) submitTo(channel, text string, atts []Attachment) tea.Cmd {
	tab := m.tabs[channel]
	if tab == nil {
		return nil
	}
	wasBusy := tab.busy
	tab.endStream()
	item := entry{role: roleYou, text: text, attachments: atts}
	item.preview = previewEscapes(m.logger(), atts, m.caps)
	tab.appendEntry(item)
	tab.busy = true
	tab.streamed = false
	tab.tokens = 0
	tab.startedAt = time.Now()
	m.syncViewport()
	turn := queuedTurn{text: text, atts: atts}
	switch {
	case tab.snapshotting:
		tab.queued = append(tab.queued, turn)
		return nil
	case wasBusy:
		m.tm.Submit(channel, tab.withShellContext(text), atts)
		return nil
	}
	tab.snapshotting = true
	tab.queued = append(tab.queued, turn)
	return m.turnStartCmd(channel)
}

func (m *model) pasteImage() bool {
	if m.clip == nil {
		return false
	}
	mime, ok := m.clip.ImageType(m.runContext())
	if !ok {
		return false
	}
	data, err := m.clip.ReadImage(m.runContext(), mime)
	if err != nil {
		m.flash = m.tr.t("tui.status.clipboard_failed", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		return true
	}
	att, err := saveClipboardImage(data, mime, m.attachSeq)
	if err != nil {
		m.flash = localizedReason(m.tr, err)
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
		m.flash = localizedReason(m.tr, err)
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

func (m *model) accountCmd(args []string) tea.Cmd {
	if len(args) == 0 {
		return m.dispatchCmd(m.active, []string{"session", "accounts", m.active})
	}
	return m.dispatchCmd(m.active, setAccountArgv(m.active, args))
}

const freshWord = "fresh"

func setAccountArgv(session string, args []string) []string {
	argv := []string{"session", "set-account", session, args[0]}
	if len(args) > 1 && args[1] == freshWord {
		argv = append(argv, freshWord)
	}
	return argv
}

func (m *model) dispatchCmd(origin string, args []string) tea.Cmd {
	backend := m.tm
	return func() tea.Msg {
		out, err := backend.Dispatch(args)
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

func (m *model) recallHistory(delta int) bool {
	if len(m.history) == 0 {
		return false
	}
	idx := m.histIdx + delta
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
	tab := m.tabs[m.active]
	if tab == nil {
		return
	}
	tab.entries = nil
	tab.streamed = false
	m.tsCache.valid = false
	m.syncViewport()
}

func (m *model) toggleHelp() {
	m.showHelp = !m.showHelp
	m.applySize()
	m.syncViewport()
}

func (m *model) sessionName(channel string) string {
	if m.tm == nil {
		return ""
	}
	sessions := m.tm.Sessions()
	for i := range sessions {
		session := &sessions[i]
		if session.ChannelID == channel && session.Name != "" {
			return session.Name
		}
	}
	return ""
}

func (m *model) activeSessionName() string {
	tab := m.tabs[m.active]
	if tab == nil {
		return ""
	}
	if name := m.sessionName(tab.channel); name != "" {
		return name
	}
	return tab.label
}

func (m *model) confirmClose() tea.Cmd {
	name := m.activeSessionName()
	if name == "" {
		return nil
	}
	return m.closeSession(name, false)
}

func (m *model) closeSession(name string, force bool) tea.Cmd {
	backend, manager := m.active, m.tm
	return func() tea.Msg {
		out, err := manager.Close(name, force)
		return dispatchResultMsg{origin: backend, out: out, err: err}
	}
}

func (m *model) closeCmd(rest []string) tea.Cmd {
	name, force := closeTarget(rest)
	if name == "" {
		name = m.activeSessionName()
	}
	if name == "" {
		m.flash = m.tr.t("tui.session.close_none", nil)
		return nil
	}
	return m.closeSession(name, force)
}

func (m *model) renameCmd(rest []string) tea.Cmd {
	target := renameTarget(rest)
	if target == "" {
		m.flash = m.tr.t("tui.session.rename_usage", nil)
		return nil
	}
	name := m.activeSessionName()
	if name == "" {
		m.flash = m.tr.t("tui.session.rename_none", nil)
		return nil
	}
	return m.dispatchCmd(m.active, []string{sessionVerb, verbRename, argName, name, argTo, target})
}

func (m *model) handoffCmd(rest []string) tea.Cmd {
	name := m.activeSessionName()
	if name == "" {
		m.flash = m.tr.t("tui.session.handoff_none", nil)
		return nil
	}
	args := []string{sessionVerb, verbHandoff, argName, name}
	if target := strings.Join(rest, " "); target != "" {
		args = append(args, argTo, target)
	}
	return m.dispatchCmd(m.active, args)
}

func renameTarget(rest []string) string {
	var words []string
	for index := 0; index < len(rest); index++ {
		tok := rest[index]
		switch {
		case tok == argTo:
			if index+1 < len(rest) && !strings.HasPrefix(rest[index+1], "--") {
				words = append(words, rest[index+1])
				index++
			}
		case strings.HasPrefix(tok, "--to="):
			words = append(words, strings.TrimPrefix(tok, "--to="))
		case !strings.HasPrefix(tok, "--"):
			words = append(words, tok)
		}
	}
	return strings.Join(words, " ")
}

func closeTarget(rest []string) (string, bool) {
	var name string
	var force bool
	for index := 0; index < len(rest); index++ {
		tok := rest[index]
		switch {
		case tok == "--force":
			force = true
		case tok == argName:
			if index+1 < len(rest) && !strings.HasPrefix(rest[index+1], "--") {
				name = rest[index+1]
				index++
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
	for index, ch := range m.order {
		if ch == m.active {
			idx = index
			break
		}
	}
	idx = (idx + delta + len(m.order)) % len(m.order)
	m.activate(m.order[idx])
}

func (m *model) activate(channel string) {
	if channel == m.active || m.tabs[channel] == nil {
		return
	}
	m.active = channel
	m.tabs[channel].unread = false
	m.tabs[channel].unreadCount = 0
	m.tabs[channel].finished = false
	m.syncViewport()
	m.vp.GotoBottom()
}

func (m *model) renderInto(target *tab, event contracts.Event) {
	if event.T != "abandoned" {
		target.disconnected = false
	}
	previous := target.tokens
	if event.Tokens > 0 && !isCompactionEvent(event.T) {
		target.tokens = event.Tokens
	}
	target.measureContext(event)
	switch event.T {
	case eventApproval:
		m.rememberApprovalDeadline(event, time.Now())
	case eventApprovalRisk:
		m.rememberApprovalRisk(event, time.Now())
	case eventApprovalSubject:
		m.rememberApprovalSubject(event, time.Now())
	case eventMenu:
		if m.openApproval(target, event) {
			return
		}
		m.renderInto(target, contracts.Event{T: eventStatus, Text: event.Text})
	case "chunk":
		target.busy = true
		target.streamed = true
		target.appendChunk(event.Text)
	case "thinking":
		target.busy = true
		target.appendThinking(event.Text, target.tokens, previous)
	case eventStatus:
		target.busy = true
		target.endStream()
		target.noteToolFile(event.Text)
		text, _ := hostError(event.Text)
		target.appendEntry(entry{role: statusRole(event.Text), text: text})
	case eventReply:
		target.appendReply(m.tr, event)
	case "todos":
		target.busy = true
		target.todos = event.Todos
	case "subagent":
		target.busy = true
		target.applySubagent(event.Subagent)
	case eventReset:
		target.busy = false
		target.interruptRequested = false
		target.streamed = false
		target.ctxMeasured = false
		target.endStream()
		target.appendEntry(entry{role: roleNotice, text: m.tr.t("tui.render.turn_reset", nil)})
	case eventNotice:
		target.appendEntry(entry{role: roleNotice, text: event.Text, markdown: true})
	case eventCompactionStarted:
		target.startCompaction(event)
	case eventCompactionDone:
		target.completeCompaction(m.tr, event)
	case eventCompactionFailed:
		target.failCompaction(event)
	case "abandoned":
		target.busy = false
		target.interruptRequested = false
		target.streamed = false
		target.ctxMeasured = false
		target.disconnected = true
		target.endStream()
		target.appendEntry(entry{role: roleNotice, text: m.tr.t("tui.render.turn_abandoned", nil)})
	}
}

func (tb *tab) measureContext(event contracts.Event) {
	used := event.TokensIn + event.CacheRead + event.CacheCreate
	tb.measureCompaction(used)
	if used <= 0 {
		return
	}
	if !event.Done {
		tb.ctxTokens, tb.ctxMeasured = used, true
	} else if !tb.ctxMeasured {
		tb.ctxTokens = used
	}
}

func (tb *tab) appendThinking(text string, tokens, before int) {
	if strings.TrimSpace(text) == "" {
		return
	}
	tb.endStream()
	if n := len(tb.entries); n > 0 && tb.entries[n-1].role == roleThinking {
		last := &tb.entries[n-1]
		last.text += text
		last.ended = time.Now()
		if tokens > 0 {
			last.tokens = tokens
		}
		return
	}
	tb.appendEntry(entry{role: roleThinking, text: text, tokens: tokens, base: before})
}

func (tb *tab) appendReply(trans translator, event contracts.Event) {
	if event.Text != "" && !tb.streamed {
		if text, isErr := hostError(event.Text); isErr {
			tb.appendEntry(entry{role: roleError, text: text})
		} else {
			tb.appendEntry(entry{role: roleAgent, text: event.Text})
		}
	}
	tb.endStream()
	if event.Cost > 0 {
		tb.lastCost = event.Cost
		tb.costTotal += event.Cost
		tb.appendEntry(entry{role: roleCost, text: formatCost(event.Cost)})
	}
	if event.Done {
		tb.finishCompaction(trans)
		tb.busy = false
		tb.interruptRequested = false
		tb.streamed = false
		tb.ctxMeasured = false
		tb.clearPanels()
	}
}

func (tb *tab) applySubagent(sub *contracts.Subagent) {
	if sub == nil {
		return
	}
	tb.recordTrail(*sub)
	now := time.Now()
	index := tb.agentIndex(sub.ID)
	if sub.State == stateDone || sub.State == stateFailed || sub.State == stateError {
		since := now
		if index >= 0 {
			since = tb.agents[index].since
			tb.dropAgent(sub.ID)
		}
		tb.agentsDone = append(tb.agentsDone, liveAgent{Subagent: *sub, since: since, ended: now})
		return
	}
	if index >= 0 {
		tb.agents[index].Subagent = *sub
		return
	}
	tb.agents = append(tb.agents, liveAgent{Subagent: *sub, since: now})
}

func (tb *tab) agentIndex(agentID string) int {
	for index, a := range tb.agents {
		if a.ID == agentID {
			return index
		}
	}
	return -1
}

func (m *model) syncViewport() {
	if !m.ready {
		return
	}
	if m.tabs[m.active] == nil {
		m.vp.SetContent(m.emptyState(m.vp.Width))
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

func (m *model) spinFrame() string { return m.st.spinFrame(m.spin) }

func (m *model) thinkingContent() string {
	return m.contentWith(m.searchQuery)
}

func (m *model) baseContent() string { return m.contentWith("") }

func (m *model) contentWith(query string) string {
	tab := m.tabs[m.active]
	if tab == nil || (len(tab.entries) == 0 && !tab.busy) {
		return m.emptyState(m.vp.Width)
	}
	content := m.cachedTranscript(tab)
	if m.foldTurnsOn {
		content = m.foldCache.get(content, "", func(s string) string {
			return strings.Join(foldTurns(m.tr, strings.Split(s, "\n"), m.tsLayout.turns, turnFoldKeep, m.st), "\n")
		})
	}
	if m.searchOpen && strings.TrimSpace(query) != "" {
		content = m.markCache.get(content, query, func(s string) string {
			return strings.Join(highlightMatches(strings.Split(s, "\n"), query), "\n")
		})
	}
	if tab.busy {
		line := m.busyRow(tab)
		if content != "" {
			content += "\n" + line
		} else {
			content = line
		}
	}
	return content
}

func (m *model) cachedTranscript(tab *tab) string {
	width := m.vp.Width
	lastLen, lastStr := 0, false
	if count := len(tab.entries); count > 0 {
		lastLen = len(tab.entries[count-1].text)
		lastStr = tab.entries[count-1].streaming
	}
	cache := &m.tsCache
	if cache.valid && cache.ch == tab.channel && cache.width == width &&
		cache.entries == len(tab.entries) && cache.lastLen == lastLen && cache.lastStr == lastStr {
		return cache.out
	}
	layout := m.layoutTranscript(tab, width)
	m.tsLayout = layout
	out := layout.text
	*cache = transcriptCache{
		ch: tab.channel, width: width, entries: len(tab.entries),
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
		return m.st.dim.Render(joinNonEmpty(
			m.labelledHint(actSelectPrevious, m.tr.t("tui.palette.navigate", nil), ctxAutocomplete),
			m.labelledHint(actSelectPageDown, m.tr.t("tui.palette.page", nil), ctxAutocomplete),
			m.labelledHint(actAutoAccept, m.tr.t("tui.palette.complete", nil), ctxAutocomplete),
			m.labelledHint(actAutoDismiss, m.tr.t("tui.common.close", nil), ctxAutocomplete),
		))
	}
	return m.st.dim.Render(m.labelledHint(actHelpToggle, m.tr.t("tui.common.help", nil), chatKeyStack()...))
}

func (m *model) footer(width int) string {
	if s := m.linkStatus(); s != "" {
		return truncate(s, width)
	}
	tab := m.tabs[m.active]
	if tab == nil {
		return ""
	}
	if tab.disconnected {
		return m.st.dim.Render(m.tr.t("tui.status.disconnected", nil))
	}
	return m.statusBar(tab, width)
}

func (m *model) spinnerHint(tab *tab) string {
	segs := []string{m.labelledHint(actCancel, m.tr.t("tui.spinner.interrupt", nil), chatKeyStack()...)}
	if !tab.startedAt.IsZero() {
		segs = append(segs, fmt.Sprintf("%ds", int(time.Since(tab.startedAt).Seconds())))
	}
	if tab.tokens > 0 {
		segs = append(segs, "↑ "+formatTokens(tab.tokens))
	}
	if tab.lastCost > 0 {
		segs = append(segs, formatCost(tab.lastCost))
	}
	return m.spinFrame() + " " + m.tr.t("tui.common.working", nil) + " (" + strings.Join(segs, " · ") + ")"
}

const minDisplayedCost = 0.01

func formatTokens(tokens int) string {
	if tokens < thousand {
		return strconv.Itoa(tokens)
	}
	return fmt.Sprintf("%.1fk", float64(tokens)/thousand)
}

func formatCost(cost float64) string {
	if cost < minDisplayedCost {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}

// Option adjusts a window before it is drawn. Options exist so a caller can say
// what the window opens on without the TUI growing a constructor per launch path.
type Option func(*model)

// Logger sends the warnings of the window to log instead of the process default logger.
func Logger(log *slog.Logger) Option {
	return func(m *model) { m.log = log }
}

func (m *model) logger() *slog.Logger {
	if m.log == nil {
		return slog.Default()
	}
	return m.log
}

// OpenOn opens the window on a named session and sends text there as its first
// message. An empty text opens the session and sends nothing, which is how a
// caller says "show me this one" without starting a turn.
func OpenOn(session, text string) Option {
	return func(m *model) { m.openSession, m.openText = session, text }
}

func newRunModel(ctx context.Context, backend Backend) *model {
	model := newModel(backend)
	model.ctx = ctx
	model.loadTheme(filepath.Dir(config.DefaultPath()), lipgloss.HasDarkBackground)
	model.loadContextLimits(config.DefaultPath(), os.Getenv)
	model.keys = &keyLog{}
	model.bar.file = model.terminalConfigPath()
	model.bar.config = loadStatuslineConfig(model.bar.file)
	model.bar.restoreFields(model.bar.config.Fields)
	return model
}

func killedByContext(ctx context.Context, err error) bool {
	return ctx.Err() != nil && errors.Is(err, tea.ErrProgramKilled)
}

func settleRun(ctx context.Context, cancel context.CancelFunc, runErr error) error {
	if killedByContext(ctx, runErr) {
		runErr = nil
	}
	cancel()
	return runErr
}

// Run starts the TUI bound to the given gateway backend, blocking until the user
// quits; quitting cancels ctx (wired by the caller) so the daemon shuts down
// cleanly.
func Run(ctx context.Context, cancel context.CancelFunc, backend Backend, opts ...Option) (runErr error) {
	model := newRunModel(ctx, backend)
	defer func() {
		runErr = errors.Join(runErr, model.releaseDrafts())
	}()
	for _, opt := range opts {
		opt(model)
	}
	if err := sweepDraftsAtStart(); err != nil {
		model.flash = model.tr.t("tui.drafts.orphans", map[string]any{tplArgReason: localizedReason(model.tr, err)})
	}
	if seam, err := newManageSeam(); err == nil {
		model.SetPluginSeam(seam)
	}
	if err := writeStdout(enhancedKeysOn); err != nil {
		cancel()
		return keyedError{cause: err, key: "tui.terminal.enhanced_keys_enable_failed", params: map[string]any{"Err": err.Error()}}
	}
	runErr = runProgram(ctx, cancel, backend, model, tea.WithInput(newFilteredStdin(model.keys)))
	if err := writeStdout(enhancedKeysOff); err != nil {
		return errors.Join(runErr, keyedError{cause: err, key: "tui.terminal.enhanced_keys_disable_failed", params: map[string]any{"Err": err.Error()}})
	}
	return runErr
}

func runProgram(ctx context.Context, cancel context.CancelFunc, backend Backend, model *model, extra ...tea.ProgramOption) error {
	options := []tea.ProgramOption{
		tea.WithContext(ctx),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithReportFocus(),
	}
	program := tea.NewProgram(model, append(options, extra...)...)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-backend.Frontend():
				if !ok {
					return
				}
				program.Send(eventMsg(entry))
			}
		}
	}()
	_, runErr := program.Run()
	return settleRun(ctx, cancel, runErr)
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tickCmd(), themeWatchTick(), enableEnhancedKeys, m.armStatusline(), fontProbeCmd())
}

func (m *model) ensureSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return spinTick(fastTick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.dispatch(msg)
	m.syncComposer()
	m.resizeComposer()
	m.applySize()
	return model, cmd
}

func (m *model) dispatch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled := m.handlePluginsMsg(msg); handled {
		return m, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if cmd, handled := m.handleKey(keyMsg); handled {
			return m, cmd
		}
	} else if cmd, handled := m.handleMsg(msg); handled {
		return m, cmd
	}
	cmd := m.updateComponents(msg)
	return m, cmd
}

func (m *model) relayout() {
	m.syncComposer()
	m.resizeComposer()
	m.applySize()
	m.syncViewport()
}

func (m *model) handleMsg(msg tea.Msg) (tea.Cmd, bool) {
	if cmd, handled := m.handleDiffMsg(msg); handled {
		return cmd, true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)
	case eventMsg:
		cmd := m.route(RoutedEvent(msg))
		return tea.Batch(cmd, m.ensureSpin()), true
	case tea.MouseMsg:
		if m.handlePaneClick(msg) {
			return nil, true
		}
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft && m.clickTranscript(msg) {
			return nil, true
		}
		if cmd, handled := m.handleModeClick(msg); handled {
			return cmd, true
		}
		if m.handleQueueClick(msg) {
			return nil, true
		}
	case imageReadyMsg:
		m.applyImage(msg)
		return nil, true
	case themeWatchMsg:
		m.watchThemes()
		return themeWatchTick(), true
	case themeEditedMsg:
		m.handleThemeEdited(msg)
		return nil, true
	case enhancedKeysFailedMsg:
		m.flash = m.tr.t("tui.status.enhanced_keys_unavailable", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
		return nil, true
	case dispatchResultMsg:
		return m.handleDispatchResult(msg), true
	case sessionStartedMsg:
		return m.handleSessionStarted(msg), true
	case modeResultMsg:
		return m.handleModeResult(msg), true
	case modeLoadedMsg:
		m.handleModeLoaded(msg)
		return nil, true
	case shellResultMsg:
		m.handleShellResult(msg)
		return nil, true
	case signalFailedMsg:
		m.flash = m.tr.t("tui.status.notify_failed", map[string]any{tplArgReason: localizedReason(m.tr, msg.err)})
		return nil, true
	case editorDoneMsg:
		m.applyEditorResult(msg)
		return nil, true
	case keybindingsDoneMsg:
		m.applyKeybindingsDone(msg)
		return nil, true
	case draftBeatFailedMsg:
		m.queueBeatFlash(msg.err)
		return m.watchDraftBeat(), true
	case compactResultMsg:
		m.handleCompactResult(msg)
		return nil, true
	case healthMsg:
		m.handleHealthMsg(msg)
		return nil, true
	case usageMsg:
		m.handleUsageMsg(msg)
		return nil, true
	case toastExpiredMsg:
		m.handleToastExpired(msg)
		return nil, true
	case toastTickMsg:
		return m.handleToastTick(msg), true
	case tea.FocusMsg:
		m.handleFocus(true)
		return nil, true
	case tea.BlurMsg:
		m.handleFocus(false)
		return nil, true
	case rewindDoneMsg:
		m.handleRewindDone(msg)
		return nil, true
	case forkDoneMsg:
		m.handleForkDone(msg)
		return nil, true
	case tickMsg:
		syncCmd := m.syncTabs()
		m.expireApprovals(time.Now())
		m.markVisibleSeen()
		m.refreshSurface()
		return tea.Batch(syncCmd, tickCmd(), m.gitTick(), m.loadModesCmd(), m.summaryRefresh(), m.healthTick()), true
	case summaryMsg:
		m.summaryBusy = false
		m.summary = panelSummary(msg)
		return nil, true
	case spinMsg:
		return m.handleSpin(), true
	case fontProbeMsg:
		m.obelFont = msg.presence
		return nil, true
	case scriptTickMsg:
		return m.handleScriptTick(), true
	case scriptDoneMsg:
		m.receiveScript(msg)
		return nil, true
	}
	return nil, false
}

func (m *model) handleWindowSize(msg tea.WindowSizeMsg) {
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
}

func (m *model) handleDispatchResult(msg dispatchResultMsg) tea.Cmd {
	cmd := m.syncTabs()
	line, role := msg.out, roleNotice
	if msg.err != nil {
		line, role = msg.err.Error(), roleError
	}
	if line == "" {
		return cmd
	}
	tab := m.tabs[msg.origin]
	if tab == nil {
		tab = m.tabs[m.active]
	}
	if tab == nil {
		m.flash = line
		return cmd
	}
	tab.appendEntry(entry{role: role, text: line})
	m.syncViewport()
	return cmd
}

func (m *model) handleSpin() tea.Cmd {
	if !m.anyBusy() {
		m.spinning = false
		return nil
	}
	m.spin++
	m.syncViewport()
	return spinTick(fastTick)
}

func (m *model) updateComponents(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	previous := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != previous {
		m.rememberComposer(previous)
	}
	cmds = append(cmds, cmd)
	m.vp, cmd = m.vp.Update(msg)
	cmds = append(cmds, cmd)
	if _, ok := msg.(tea.KeyMsg); ok {
		m.resizeComposer()
		if m.paletteOpen() || m.palWasOpen {
			m.clampPal()
			m.relayout()
		}
		m.palWasOpen = m.paletteOpen()
	}
	return tea.Batch(cmds...)
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	m.keys.write("key type=%d alt=%v str=%q", msg.Type, msg.Alt, msg.String())
	m.flash = ""
	cmd, handled := m.dispatchKey(msg)
	if m.flash == "" {
		m.flash = m.takePendingBeatFlash()
	}
	return cmd, handled
}

func (m *model) dispatchKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.toast.title != "" {
		if run, ok := m.tables().toast[m.directAction(msg, ctxToast)]; ok {
			return run(m, msg), true
		}
	}
	switch {
	case m.diff.open:
		return m.handleDiffKey(msg), true
	case m.pendingClose:
		return m.handlePendingClose(msg), true
	case m.planMenu != nil:
		return m.handlePlanKey(msg), true
	case m.surface == surfaceBoard:
		return m.handleBoardKey(msg), true
	case m.switchOpen:
		return m.handleSwitchKey(msg), true
	case m.searchOpen:
		return m.handleSearchKey(msg), true
	case m.diagOpen:
		return m.handleDiagKey(msg), true
	case m.ctxOpen:
		return m.handleContextKey(msg), true
	case m.health.open:
		return m.handleHealthKey(msg), true
	case m.usage.open:
		return m.handleUsageKey(msg), true
	case m.pluginsOpen:
		return m.handlePluginsOverlayKey(msg), true
	case m.skillsOpen:
		return m.handleSkillsKey(msg), true
	case m.resumeOpen:
		return m.handleResumeKey(msg), true
	case m.themeScr != nil:
		return m.handleThemeKey(msg), true
	case m.bar.open:
		return m.handleStatuslineKey(msg), true
	case m.histOpen:
		return m.handleHistSearchKey(msg), true
	case m.rwOpen:
		return m.handleRewindKey(msg), true
	}
	if m.handleApprovalKey(msg) {
		return nil, true
	}
	if m.surface == surfacePanes {
		if cmd, handled := m.handlePanesKey(msg); handled {
			return cmd, true
		}
	}
	if cmd, handled := m.handlePaletteKey(msg); handled {
		return cmd, true
	}
	if m.handleMentionKey(msg) {
		return nil, true
	}
	return m.handleMainKey(msg)
}

func (m *model) handlePendingClose(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxClose)
	m.pendingClose = false
	if run, ok := m.tables().pendingClose[action]; ok {
		return run(m, msg)
	}
	return nil
}

func pendingCloseRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSessionClose, cmdKey(func(app *model) tea.Cmd { return app.confirmClose() })),
		bind[keyRun](actInterrupt, cmdKey(func(app *model) tea.Cmd {
			cmd, _ := app.interruptOrQuit(actInterrupt)
			return cmd
		})),
		bind[keyRun](actExit, cmdKey(func(app *model) tea.Cmd {
			cmd, _ := app.interruptOrQuit(actExit)
			return cmd
		})),
	)
}

func (m *model) handleSwitchKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxSessions)
	if m.overlayNav(action, m.moveSwitch) {
		return nil
	}
	if run, ok := m.tables().switchOverlay[action]; ok {
		return run(m, msg)
	}
	return nil
}

func switchOverlayRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSelectCancel, stepKey(func(app *model) {
			app.switchOpen = false
			app.relayout()
		})),
		bind[keyRun](actSelectAccept, cmdKey(func(app *model) tea.Cmd {
			app.chooseSwitch()
			app.switchOpen = false
			app.relayout()
			return app.ensureSpin()
		})),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxSearch)
	if run, ok := m.tables().search[action]; ok {
		return run(m, msg)
	}
	if action != "" {
		return nil
	}
	switch msg.Type {
	case tea.KeyBackspace:
		if q := m.searchQuery; q != "" {
			m.typeSearch(q[:len(q)-1])
		}
	case tea.KeyRunes:
		if !msg.Alt {
			m.typeSearch(m.searchQuery + string(msg.Runes))
		}
	case tea.KeySpace:
		m.typeSearch(m.searchQuery + " ")
	}
	return nil
}

func searchRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actSearchClose, stepKey((*model).closeSearch)),
		bind[keyRun](actSearchNext, stepKey(func(app *model) { app.searchStep(1) })),
		bind[keyRun](actSearchPrevious, stepKey(func(app *model) { app.searchStep(-1) })),
	)
}

func (m *model) handleDiagKey(msg tea.KeyMsg) tea.Cmd {
	chosen := m.stackAction(msg, ctxDiagnostics)
	if run, ok := m.tables().diag[chosen]; ok {
		return run(m, msg)
	}
	return nil
}

func diagRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
		bind[keyRun](actDiagnosticsClose, stepKey(func(app *model) {
			app.diagOpen = false
			app.relayout()
		})),
	)
}

func (m *model) handlePluginsOverlayKey(msg tea.KeyMsg) tea.Cmd {
	return m.handlePluginsKey(msg)
}

func (m *model) handleSkillsKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxSkills)
	if m.overlayNav(action, m.moveSkills) {
		return nil
	}
	if run, ok := m.tables().skills[action]; ok {
		return run(m, msg)
	}
	return nil
}

func skillsRuns() (map[keyAction]keyRun, error) {
	closeSkills := stepKey(func(app *model) {
		app.skillsOpen = false
		app.relayout()
	})
	return dispatchOf(
		bind[keyRun](actSelectCancel, closeSkills),
		bind[keyRun](actSelectAccept, closeSkills),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handleResumeKey(msg tea.KeyMsg) tea.Cmd {
	action := m.stackAction(msg, ctxResume)
	if m.overlayNav(action, m.moveResume) {
		return nil
	}
	if run, ok := m.tables().resume[action]; ok {
		return run(m, msg)
	}
	if action != "" {
		return nil
	}
	switch msg.Type {
	case tea.KeyBackspace:
		m.eraseResume()
	case tea.KeySpace:
		m.typeResume(" ")
	case tea.KeyRunes:
		m.typeResume(string(msg.Runes))
	}
	return nil
}

func resumeRuns() (map[keyAction]keyRun, error) {
	return dispatchOf(
		bind[keyRun](actSelectCancel, stepKey(func(app *model) {
			app.resumeOpen = false
			app.relayout()
		})),
		bind[keyRun](actSelectAccept, cmdKey(func(app *model) tea.Cmd {
			cmd := app.chooseResume()
			app.resumeOpen = false
			app.relayout()
			return tea.Batch(cmd, app.ensureSpin())
		})),
		bind[keyRun](actInterrupt, cmdKey((*model).overlayCtrlC)),
	)
}

func (m *model) handlePaletteKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !m.paletteOpen() {
		return nil, false
	}
	action := m.stackAction(msg, ctxAutocomplete)
	if m.overlayNav(action, m.movePal) {
		return nil, true
	}
	if run, ok := m.tables().palette[action]; ok {
		return run(m, msg)
	}
	return nil, false
}

func paletteRuns() (map[keyAction]chatRun, error) {
	return dispatchOf(
		bind[chatRun](actAutoAccept, stepChat(func(app *model) {
			app.completePal()
			app.relayout()
		})),
		bind[chatRun](actAutoDismiss, stepChat(func(app *model) {
			app.input.SetValue("")
			app.palIdx = 0
			app.relayout()
		})),
		bind[chatRun](actSelectAccept, func(app *model, _ tea.KeyMsg) (tea.Cmd, bool) {
			cmd := app.handleEnter()
			app.palIdx = 0
			app.relayout()
			return tea.Batch(cmd, app.ensureSpin()), true
		}),
		bind[chatRun](actInterrupt, chatCmd((*model).overlayCtrlC)),
	)
}

func (m *model) handleMentionKey(msg tea.KeyMsg) bool {
	if !m.mentionOpen() {
		return false
	}
	chosen := m.stackAction(msg, ctxMention)
	if run, ok := m.tables().mention[chosen]; ok {
		return run(m, msg)
	}
	return false
}

func mentionRuns() (map[keyAction]flagRun, error) {
	return dispatchOf(
		bind[flagRun](actSelectPrevious, stepFlag(func(app *model) { app.moveMention(-1) })),
		bind[flagRun](actSelectNext, stepFlag(func(app *model) { app.moveMention(1) })),
		bind[flagRun](actAutoAccept, stepFlag(func(app *model) {
			app.completeActiveMention()
			app.relayout()
		})),
	)
}

func (m *model) handleMainKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.handleQueueFocus(msg) {
		return nil, true
	}
	action, matched := m.pressChatKey(msg, time.Now())
	if !matched {
		return nil, false
	}
	if action == "" {
		return nil, true
	}
	return m.runChatAction(action, msg)
}

func (m *model) escapeKey() {
	if tb := m.tabs[m.active]; tb != nil && tb.busy {
		if name := m.sessionName(m.active); name != "" {
			m.tm.Interrupt(name)
		}
		return
	}
	m.escapeIdle()
}

func (m *model) enterKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.Alt {
		return nil, m.enqueueWhileBusy(true)
	}
	if m.continueLine() || m.enqueueWhileBusy(false) {
		return nil, true
	}
	return tea.Batch(m.handleEnter(), m.ensureSpin()), true
}

func (m *model) removePendingKey() (tea.Cmd, bool) {
	if !m.removeLastPending() {
		return nil, false
	}
	m.relayout()
	return nil, true
}

func (m *model) upKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.focusQueueStart() {
		return nil, true
	}
	return m.handleHistoryKey(msg, -1)
}

func (m *model) closeTabKey() (tea.Cmd, bool) {
	if m.input.Value() != "" {
		return nil, false
	}
	m.pendingClose = true
	return nil, true
}

const quitArmWindow = 3 * time.Second

func (m *model) interruptOrQuit(pressed keyAction) (tea.Cmd, bool) {
	turn := m.tabs[m.active]
	if turn != nil && turn.busy && !turn.interruptRequested {
		turn.interruptRequested = true
		if !m.interruptActive() {
			m.flash = m.tr.t("tui.status.nothing_to_interrupt", map[string]any{tplArgKey: m.hintOf(pressed)})
		}
		return nil, true
	}
	if busy := m.busySessions(); busy > 0 && time.Now().After(m.quitArmedUntil) {
		m.quitArmedUntil = time.Now().Add(quitArmWindow)
		m.flash = m.tr.t("tui.status.quit_busy", map[string]any{tplArgCount: busy, tplArgKey: m.hintOf(pressed)})
		return nil, true
	}
	return tea.Quit, true
}

func (m *model) busySessions() int {
	busy := 0
	for name, tb := range m.tabs {
		if tb.busy && (name != m.active || !tb.interruptRequested) {
			busy++
		}
	}
	return busy
}

func (m *model) interruptActive() bool {
	name := m.sessionName(m.active)
	return name != "" && m.tm.Interrupt(name)
}

func (m *model) overlayCtrlC() tea.Cmd {
	cmd, _ := m.interruptOrQuit(actInterrupt)
	return cmd
}

func (m *model) handleHistoryKey(_ tea.KeyMsg, dir int) (tea.Cmd, bool) {
	if dir < 0 {
		if m.input.LineCount() <= 1 && (m.input.Value() == "" || m.histIdx < len(m.history)) && m.recallHistory(-1) {
			return nil, true
		}
		return nil, false
	}
	if m.input.LineCount() <= 1 && m.histIdx < len(m.history) && m.recallHistory(1) {
		return nil, true
	}
	return nil, false
}

func (m *model) helpView() string {
	help := m.keyHelpLine()
	if m.width > 0 {
		help = foldText(help, m.width)
	}
	return m.st.dim.Render(help)
}

func (m *model) View() string {
	if !m.ready {
		return m.tr.t("tui.common.starting", nil)
	}
	if m.diff.open {
		return m.diffScreen()
	}
	if m.themeScr != nil {
		return m.themeScreenView()
	}
	if m.surface != surfaceChat {
		return m.surfaceScreen()
	}
	body := ""
	if m.vp.Height > 0 {
		body = m.vp.View()
	}
	if side := m.sidebarPane(m.vp.Height); side != nil {
		body = m.joinSidebar(body, side)
	}
	return m.overlayToast(m.st.finish(m.buildFrame().render(body)))
}

func (tb *tab) recordTrail(sub contracts.Subagent) {
	now := time.Now()
	for index := range tb.trail {
		if tb.trail[index].ID == sub.ID {
			tb.trail[index].Subagent = sub
			if isTerminalAgent(sub.State) && tb.trail[index].ended.IsZero() {
				tb.trail[index].ended = now
			}
			return
		}
	}
	tb.trail = append(tb.trail, liveAgent{Subagent: sub, since: now})
}

func isTerminalAgent(state string) bool {
	return state == stateDone || state == stateFailed || state == stateTimeout
}

func foldableEntry(item entry) bool {
	switch item.role {
	case roleThinking:
		return item.text != ""
	case roleTool:
		return strings.Contains(item.text, "\n")
	}
	return false
}

func (m *model) openOrFold() {
	if _, ok := m.selectedLink(); ok {
		m.openSelectedLink()
		return
	}
	tab := m.tabs[m.active]
	if tab == nil {
		return
	}
	for index := len(tab.entries) - 1; index >= 0; index-- {
		if foldableEntry(tab.entries[index]) {
			m.toggleEntryFold(tab, tab.entries[index].id)
			return
		}
	}
	m.flash = m.tr.t("tui.render.nothing_to_fold", nil)
}

func (m *model) toggleEntryFold(tab *tab, entryID uint64) {
	if tab.flips == nil {
		tab.flips = make(map[uint64]bool)
	}
	tab.flips[entryID] = !tab.flips[entryID]
	m.tsCache.valid = false
	m.syncViewport()
}

func (m *model) frameTop() int {
	top := 0
	for _, line := range m.buildFrame().above {
		top += strings.Count(line, "\n") + 1
	}
	return top
}

func (m *model) clickTranscript(msg tea.MouseMsg) bool {
	tab := m.tabs[m.active]
	if tab == nil || m.foldTurnsOn {
		return false
	}
	row := msg.Y - m.frameTop() + m.vp.YOffset
	if row < 0 || row >= len(m.tsLayout.owners) {
		return false
	}
	owner := m.tsLayout.owners[row]
	for index := range tab.entries {
		if tab.entries[index].id == owner && owner != 0 && foldableEntry(tab.entries[index]) {
			m.toggleEntryFold(tab, owner)
			return true
		}
	}
	return false
}

func (m *model) busyRow(tab *tab) string {
	elapsedText := ""
	if !tab.startedAt.IsZero() {
		elapsedText = elapsed(time.Since(tab.startedAt))
	}
	tokens := ""
	if tab.tokens > 0 {
		tokens = formatTokens(tab.tokens)
	}
	cost := ""
	if tab.costTotal > 0 {
		cost = formatCost(tab.costTotal)
	}
	return busyLine(m.tr, m.vp.Width, m.st, m.spinFrame(), elapsedText, tokens, cost, m.labelledHint(actCancel, m.tr.t("tui.common.interrupt", nil), chatKeyStack()...))
}
