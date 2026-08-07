package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PluginRow is one compiled-in plugin as the screen shows it: what is built in,
// what is published, and whether the operator asked for it to be left alone.
type PluginRow struct {
	Module    string
	Installed string
	Latest    string
	Pinned    bool
}

// PluginAction is one change the screen can ask for.
type PluginAction string

const (
	PluginBump       PluginAction = "bump"
	PluginSetVersion PluginAction = "version"
	PluginPin        PluginAction = "pin"
	PluginUnpin      PluginAction = "unpin"
	PluginRemove     PluginAction = "remove"
)

// PluginSeam is the narrow view of the host's plugin management the screen
// drives. It is declared here rather than imported so the screen is testable
// against a fake and the TUI never depends on the Go toolchain.
//
// The seam is split into phases rather than handed a decision callback: an
// Update loop cannot block on a menu it has not drawn yet, so the screen keeps
// both decisions and calls Restore itself when the operator asks for it.
type PluginSeam interface {
	List(ctx context.Context) ([]PluginRow, error)
	Versions(ctx context.Context, module string) ([]string, error)
	Findings(ctx context.Context, module, version string) []string
	Apply(ctx context.Context, action PluginAction, module, version string) (output string, err error)
	Restore(ctx context.Context) error
}

// The menu labels are constants because they are also the values the key
// handler switches on, and a menu whose rows and branches can drift apart is a
// menu that eventually runs the wrong thing.
const (
	pluginActionBump    = "bump to the latest version"
	pluginActionPin     = "pin at the installed version"
	pluginActionUnpin   = "unpin"
	pluginActionVersion = "version…"
	pluginActionRemove  = "remove from the composition"

	pluginWarnProceed = "proceed"
	pluginWarnAbort   = "cancel, change nothing"

	pluginFailRestore = "restore the tree as it was"
	pluginFailKeep    = "keep it, I will repair by hand"
)

// pluginsMode is where the screen is in the one flow it has: pick a plugin, pick
// an action, confirm, and answer for the build.
type pluginsMode int

const (
	pluginsList pluginsMode = iota
	pluginsActions
	pluginsVersions
	pluginsWarn
	pluginsFailed
	pluginsDone
)

// pluginsMax bounds how many plugin rows the screen shows at once.
const pluginsMax = 10

// pluginsWindow picks the slice of rows to draw around the selection. The list
// scrolls rather than truncating: a cursor the arrows can reach but the screen
// does not draw is a selection the operator cannot see.
func pluginsWindow(idx, n int) (start, end int) {
	if n <= pluginsMax {
		return 0, n
	}
	start = idx - pluginsMax/2
	if start > n-pluginsMax {
		start = n - pluginsMax
	}
	if start < 0 {
		start = 0
	}
	return start, start + pluginsMax
}

// pluginsRequest is the change waiting on the operator's two answers.
type pluginsRequest struct {
	action  PluginAction
	module  string
	version string
}

// Messages carrying the seam's answers back into the Update loop, so nothing the
// screen does — a version listing, a build — blocks the event loop.
type (
	pluginsListedMsg   struct{ rows []PluginRow }
	pluginsVersionsMsg struct{ versions []string }
	pluginsFindingsMsg struct{ findings []string }
	pluginsAppliedMsg  struct {
		output string
		err    error
	}
	pluginsRestoredMsg struct{}
	pluginsErrMsg      struct{ err error }
)

// SetPluginSeam gives the screen its way into the host's plugin management. It
// is set from outside so a build without one still runs the TUI.
func (m *model) SetPluginSeam(s PluginSeam) { m.pluginsSeam = s }

// openPlugins opens the screen and asks the seam for the listing off the Update
// loop: resolving published versions needs the network, and the composition is
// not worth freezing the terminal over.
func (m *model) openPlugins() tea.Cmd {
	m.pluginsOpen = true
	m.pluginsMode = pluginsList
	m.pluginsIdx = 0
	m.pluginsRows = nil
	m.pluginsNotice = ""
	if m.pluginsSeam == nil {
		m.pluginsNotice = "plugin management is not available in this build"
		return nil
	}
	seam := m.pluginsSeam
	return func() tea.Msg {
		rows, err := seam.List(context.Background())
		if err != nil {
			return pluginsErrMsg{err: err}
		}
		return pluginsListedMsg{rows: rows}
	}
}

// handlePluginsMsg folds a seam answer into the screen. It returns false for a
// message the screen has no business with, so Update can carry on.
func (m *model) handlePluginsMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case pluginsListedMsg:
		m.pluginsRows = msg.rows
		m.clampPlugins()
	case pluginsVersionsMsg:
		m.pluginsMenu = msg.versions
		m.pluginsMenuIdx = len(msg.versions) - 1 // the newest is the usual answer
		m.clampPluginsMenu()
		m.pluginsMode = pluginsVersions
	case pluginsFindingsMsg:
		m.pluginsFindings = msg.findings
		m.pluginsMenu = []string{pluginWarnProceed, pluginWarnAbort}
		m.pluginsMenuIdx = 0
		m.pluginsMode = pluginsWarn
	case pluginsAppliedMsg:
		m.pluginsOutput = msg.output
		if msg.err != nil {
			m.pluginsMenu = []string{pluginFailRestore, pluginFailKeep}
			m.pluginsMenuIdx = 0
			m.pluginsMode = pluginsFailed
			break
		}
		m.pluginsNotice = "the change is on disk; it applies at the next restart"
		m.pluginsMode = pluginsDone
	case pluginsRestoredMsg:
		m.pluginsNotice = "the tree is back as it was; nothing was installed"
		m.pluginsMode = pluginsDone
	case pluginsErrMsg:
		m.pluginsNotice = msg.err.Error()
		m.pluginsMode = pluginsDone
	default:
		return nil, false
	}
	m.applySize()
	m.syncViewport()
	return nil, true
}

// handlePluginsKey drives the screen. It is modal: every key belongs to it while
// it is open.
func (m *model) handlePluginsKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyUp:
		m.movePlugins(-1)
	case tea.KeyDown:
		m.movePlugins(1)
	case tea.KeyEsc:
		m.backPlugins()
	case tea.KeyEnter:
		return m.choosePlugins()
	}
	return nil
}

// backPlugins steps out of the current mode, closing the screen from the top.
// A mid-flow Esc drops the pending change: nothing has been written yet.
func (m *model) backPlugins() {
	switch m.pluginsMode {
	case pluginsList:
		m.pluginsOpen = false
	case pluginsVersions:
		m.openPluginActions()
	default:
		m.pluginsMode = pluginsList
	}
	m.applySize()
	m.syncViewport()
}

// choosePlugins acts on the selected row of whatever is on screen.
func (m *model) choosePlugins() tea.Cmd {
	switch m.pluginsMode {
	case pluginsList:
		m.openPluginActions()
		return nil
	case pluginsActions:
		return m.chooseAction()
	case pluginsVersions:
		if v := m.selectedMenu(); v != "" {
			return m.request(pluginsRequest{action: PluginSetVersion, module: m.selectedModule(), version: v})
		}
	case pluginsWarn:
		if m.selectedMenu() == pluginWarnProceed {
			return m.runRequest()
		}
		m.pluginsMode = pluginsList
	case pluginsFailed:
		if m.selectedMenu() == pluginFailRestore {
			seam := m.pluginsSeam
			return func() tea.Msg {
				if err := seam.Restore(context.Background()); err != nil {
					return pluginsErrMsg{err: err}
				}
				return pluginsRestoredMsg{}
			}
		}
		m.pluginsNotice = "the tree is left as it is; repair it and run the build again"
		m.pluginsMode = pluginsDone
	case pluginsDone:
		return m.openPlugins()
	}
	return nil
}

// openPluginActions builds the action menu for the selected plugin. Pin and
// unpin are one row, not two: only one of them is ever the answer.
func (m *model) openPluginActions() {
	row := m.selectedRow()
	if row.Module == "" {
		return
	}
	pin := pluginActionPin
	if row.Pinned {
		pin = pluginActionUnpin
	}
	m.pluginsMenu = []string{pluginActionBump, pin, pluginActionVersion, pluginActionRemove}
	m.pluginsMenuIdx = 0
	m.pluginsMode = pluginsActions
	m.applySize()
	m.syncViewport()
}

func (m *model) chooseAction() tea.Cmd {
	module := m.selectedModule()
	switch m.selectedMenu() {
	case pluginActionBump:
		return m.request(pluginsRequest{action: PluginBump, module: module})
	case pluginActionPin:
		return m.request(pluginsRequest{action: PluginPin, module: module})
	case pluginActionUnpin:
		return m.request(pluginsRequest{action: PluginUnpin, module: module})
	case pluginActionRemove:
		return m.request(pluginsRequest{action: PluginRemove, module: module})
	case pluginActionVersion:
		seam := m.pluginsSeam
		return func() tea.Msg {
			vs, err := seam.Versions(context.Background(), module)
			if err != nil {
				return pluginsErrMsg{err: err}
			}
			return pluginsVersionsMsg{versions: vs}
		}
	}
	return nil
}

// request records the pending change and asks the seam what is known against it.
// The confirmation is asked even when nothing is found, because "nothing known
// against it" is not the same as "it will work".
func (m *model) request(req pluginsRequest) tea.Cmd {
	m.pluginsReq = req
	seam := m.pluginsSeam
	return func() tea.Msg {
		return pluginsFindingsMsg{findings: seam.Findings(context.Background(), req.module, req.version)}
	}
}

func (m *model) runRequest() tea.Cmd {
	seam, req := m.pluginsSeam, m.pluginsReq
	return func() tea.Msg {
		out, err := seam.Apply(context.Background(), req.action, req.module, req.version)
		return pluginsAppliedMsg{output: out, err: err}
	}
}

func (m *model) selectedRow() PluginRow {
	if m.pluginsIdx < 0 || m.pluginsIdx >= len(m.pluginsRows) {
		return PluginRow{}
	}
	return m.pluginsRows[m.pluginsIdx]
}

func (m *model) selectedModule() string { return m.selectedRow().Module }

func (m *model) selectedMenu() string {
	if m.pluginsMenuIdx < 0 || m.pluginsMenuIdx >= len(m.pluginsMenu) {
		return ""
	}
	return m.pluginsMenu[m.pluginsMenuIdx]
}

// movePlugins moves the selection of whichever list is on screen.
func (m *model) movePlugins(d int) {
	if m.pluginsMode == pluginsList {
		m.pluginsIdx += d
		m.clampPlugins()
		return
	}
	m.pluginsMenuIdx += d
	m.clampPluginsMenu()
}

func (m *model) clampPlugins() {
	if m.pluginsIdx >= len(m.pluginsRows) {
		m.pluginsIdx = len(m.pluginsRows) - 1
	}
	if m.pluginsIdx < 0 {
		m.pluginsIdx = 0
	}
}

func (m *model) clampPluginsMenu() {
	if m.pluginsMenuIdx >= len(m.pluginsMenu) {
		m.pluginsMenuIdx = len(m.pluginsMenu) - 1
	}
	if m.pluginsMenuIdx < 0 {
		m.pluginsMenuIdx = 0
	}
}

// pluginsView renders the screen as an inline Claude menu (no border box): a dim
// header, the plugin rows, and whichever menu the flow is asking through.
func (m *model) pluginsView() string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("plugins — ↑↓ select · Enter choose · Esc back"))
	if m.pluginsNotice != "" {
		b.WriteString("\n" + dimStyle.Render("  "+m.pluginsNotice))
	}
	if len(m.pluginsRows) == 0 && m.pluginsNotice == "" {
		b.WriteString("\n" + dimStyle.Render("  (no plugins compiled in)"))
	}
	start, end := pluginsWindow(m.pluginsIdx, len(m.pluginsRows))
	if start > 0 {
		b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d above", start)))
	}
	for i := start; i < end; i++ {
		r := m.pluginsRows[i]
		row := fmt.Sprintf("%s · %s → %s", r.Module, r.Installed, r.Latest)
		if r.Pinned {
			row += " · pinned"
		}
		if i == m.pluginsIdx && m.pluginsMode == pluginsList {
			b.WriteString("\n" + accentStyle.Render(glyphCursor+" "+row))
		} else {
			b.WriteString("\n" + dimStyle.Render("  "+row))
		}
	}
	if end < len(m.pluginsRows) {
		b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … %d below", len(m.pluginsRows)-end)))
	}
	if m.pluginsMode == pluginsWarn {
		for _, f := range m.pluginsFindings {
			b.WriteString("\n" + textStyle.Render("  ! "+f))
		}
	}
	// The build's own output, not a paraphrase of it: it is the only thing that
	// says why the composition was refused.
	if m.pluginsMode == pluginsFailed && m.pluginsOutput != "" {
		for _, line := range strings.Split(strings.TrimRight(m.pluginsOutput, "\n"), "\n") {
			b.WriteString("\n" + dimStyle.Render("  "+line))
		}
	}
	if m.pluginsMode != pluginsList && m.pluginsMode != pluginsDone {
		for i, opt := range m.pluginsMenu {
			if i == m.pluginsMenuIdx {
				b.WriteString("\n" + accentStyle.Render(glyphCursor+" "+opt))
			} else {
				b.WriteString("\n" + dimStyle.Render("  "+opt))
			}
		}
	}
	return b.String()
}

// pluginsHeight is the rendered row count of the open screen (0 when closed), so
// chromeHeight can reserve space for it.
func (m *model) pluginsHeight() int {
	if !m.pluginsOpen {
		return 0
	}
	return lipgloss.Height(m.pluginsView())
}
