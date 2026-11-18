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
