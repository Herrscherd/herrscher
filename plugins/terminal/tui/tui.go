// Package tui renders a gateway's live event stream and captures the operator's
// input, driving it through the narrow Backend interface. It is the terminal
// gateway's frontend: the daemon hub treats that gateway like any other; the TUI
// is what makes it a human-usable pane. Depending on Backend (not the concrete
// gateway) keeps this package importable by the gateway without a cycle.
//
// Rendering is split by concern: tabs.go maps bus events onto transcript roles,
// render.go draws one entry per role, markdown.go runs the agent's prose through
// the markdown engine (and its diffs by hand), chrome.go frames the flow, and
// status.go derives what the session has cost and how full its context is. The
// palette lives in theme.go, so no render site names a colour.
package tui

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
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
	Scrollback(name string) ([]contracts.ScrollbackLine, error)
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

	sessionInfos map[string]contracts.SessionInfo

	open     openWait
	clock    func() time.Time
	starting bool

	heldSession string

	sessionsIssued   uint64
	sessionsApplied  uint64
	sessionsInFlight bool

	historySeq uint64

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

	resumeAll          []contracts.SessionInfo
	resumeQuery        string
	resumePreviewLines []string

	surface     surface
	board       boardState
	overviews   []SessionOverview
	summary     panelSummary
	summaryBusy bool
	panes       paneState

	boardInfos      []contracts.SessionInfo
	surfaceIssued   uint64
	surfaceApplied  uint64
	surfaceInFlight bool

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

func (m *model) runContext() context.Context {
	if m.ctx == nil {
		return context.Background()
	}
	return m.ctx
}

func newModel(messages *clitext.Messages, backend Backend) *model {
	trans := newTranslator(messages)
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

func newRunModel(ctx context.Context, messages *clitext.Messages, backend Backend) *model {
	model := newModel(messages, backend)
	model.ctx = ctx
	model.loadTheme(filepath.Dir(config.DefaultPath()), lipgloss.HasDarkBackground)
	model.loadContextLimits(config.DefaultPath(), os.Getenv)
	model.keys = &keyLog{}
	model.bar.file = model.terminalConfigPath()
	model.bar.config = loadStatuslineConfig(model.bar.file)
	model.bar.restoreFields(model.bar.config.Fields)
	return model
}

// Run starts the TUI bound to the given gateway backend, blocking until the user
// quits; quitting cancels ctx (wired by the caller) so the daemon shuts down
// cleanly.
func Run(ctx context.Context, cancel context.CancelFunc, messages *clitext.Messages, backend Backend, opts ...Option) (runErr error) {
	model := newWindow(ctx, messages, backend, opts)
	defer func() {
		runErr = errors.Join(runErr, model.releaseDrafts())
	}()
	if err := sweepDraftsAtStart(); err != nil {
		model.flash = model.tr.t("tui.drafts.orphans", map[string]any{tplArgReason: localizedReason(model.tr, err)})
	}
	if seam, err := newManageSeam(messages); err == nil {
		model.SetPluginSeam(seam)
	}
	if err := writeStdout(enhancedKeysOn); err != nil {
		cancel()
		return renderedError{cause: err, text: model.tr.t("tui.terminal.enhanced_keys_enable_failed", map[string]any{"Err": err.Error()})}
	}
	runErr = runProgram(ctx, cancel, backend, model, tea.WithInput(newFilteredStdin(model.keys)))
	if err := writeStdout(enhancedKeysOff); err != nil {
		return errors.Join(runErr, renderedError{cause: err, text: model.tr.t("tui.terminal.enhanced_keys_disable_failed", map[string]any{"Err": err.Error()})})
	}
	return runErr
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(m.syncTabs(), m.drainHistory(), textarea.Blink, tickCmd(), themeWatchTick(), enableEnhancedKeys, m.armStatusline(), fontProbeCmd())
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
