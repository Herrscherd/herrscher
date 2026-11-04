package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// The plugin changes the screen offers.
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

const (
	pluginActionBump    = "passer à la dernière version"
	pluginActionPin     = "épingler la version installée"
	pluginActionUnpin   = "désépingler"
	pluginActionVersion = "version…"
	pluginActionRemove  = "retirer de la composition"

	pluginWarnProceed = "continuer"
	pluginWarnAbort   = "annuler, ne rien changer"

	pluginFailRestore = "remettre l’arbre comme il était"
	pluginFailKeep    = "le garder, je répare à la main"
)

type pluginsMode int

const (
	pluginsList pluginsMode = iota
	pluginsActions
	pluginsVersions
	pluginsWarn
	pluginsFailed
	pluginsDone
)

const pluginsMax = 10

const centreDivisor = 2

func pluginsWindow(count, total int) (int, int) {
	if total <= pluginsMax {
		return 0, total
	}
	start := count - pluginsMax/centreDivisor
	if start > total-pluginsMax {
		start = total - pluginsMax
	}
	if start < 0 {
		start = 0
	}
	return start, start + pluginsMax
}

type pluginsRequest struct {
	action  PluginAction
	module  string
	version string
}

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

func (m *model) SetPluginSeam(s PluginSeam) { m.pluginsSeam = s }

func (m *model) openPlugins() tea.Cmd {
	m.pluginsOpen = true
	m.pluginsMode = pluginsList
	m.pluginsIdx = 0
	m.pluginsRows = nil
	m.pluginsNotice = ""
	if m.pluginsSeam == nil {
		m.pluginsNotice = "la gestion des plugins n’est pas disponible dans ce binaire"
		return nil
	}
	seam, ctx := m.pluginsSeam, m.runContext()
	return func() tea.Msg {
		rows, err := seam.List(ctx)
		if err != nil {
			return pluginsErrMsg{err: err}
		}
		return pluginsListedMsg{rows: rows}
	}
}

func (m *model) handlePluginsMsg(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case pluginsListedMsg:
		m.pluginsRows = msg.rows
		m.clampPlugins()
	case pluginsVersionsMsg:
		m.pluginsMenu = msg.versions
		m.pluginsMenuIdx = len(msg.versions) - 1
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
		m.pluginsNotice = "le changement est sur le disque ; il s’applique au prochain démarrage"
		m.pluginsMode = pluginsDone
	case pluginsRestoredMsg:
		m.pluginsNotice = "l’arbre est revenu comme il était ; rien n’a été installé"
		m.pluginsMode = pluginsDone
	case pluginsErrMsg:
		m.pluginsNotice = msg.err.Error()
		m.pluginsMode = pluginsDone
	default:
		return false
	}
	m.applySize()
	m.syncViewport()
	return true
}

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

func (m *model) backPlugins() {
	switch m.pluginsMode {
	case pluginsList:
		m.pluginsOpen = false
	case pluginsVersions:
		m.openPluginActions()
	case pluginsActions, pluginsWarn, pluginsFailed, pluginsDone:
		m.pluginsMode = pluginsList
	}
	m.applySize()
	m.syncViewport()
}

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
			seam, ctx := m.pluginsSeam, m.runContext()
			return func() tea.Msg {
				if err := seam.Restore(ctx); err != nil {
					return pluginsErrMsg{err: err}
				}
				return pluginsRestoredMsg{}
			}
		}
		m.pluginsNotice = "l’arbre est laissé tel quel ; répare-le et relance la construction"
		m.pluginsMode = pluginsDone
	case pluginsDone:
		return m.openPlugins()
	}
	return nil
}

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
		seam, ctx := m.pluginsSeam, m.runContext()
		return func() tea.Msg {
			versions, err := seam.Versions(ctx, module)
			if err != nil {
				return pluginsErrMsg{err: err}
			}
			return pluginsVersionsMsg{versions: versions}
		}
	}
	return nil
}

func (m *model) request(req pluginsRequest) tea.Cmd {
	m.pluginsReq = req
	seam, ctx := m.pluginsSeam, m.runContext()
	return func() tea.Msg {
		return pluginsFindingsMsg{findings: seam.Findings(ctx, req.module, req.version)}
	}
}

func (m *model) runRequest() tea.Cmd {
	seam, req, ctx := m.pluginsSeam, m.pluginsReq, m.runContext()
	return func() tea.Msg {
		out, err := seam.Apply(ctx, req.action, req.module, req.version)
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

func (m *model) movePlugins(delta int) {
	if m.pluginsMode == pluginsList {
		m.pluginsIdx += delta
		m.clampPlugins()
		return
	}
	m.pluginsMenuIdx += delta
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

func (m *model) pluginsView() string {
	var builder strings.Builder
	builder.WriteString(m.st.dim.Render("plugins · ↑↓ naviguer · ⏎ choisir · échap retour"))
	m.writePluginNotices(&builder)
	m.writePluginRows(&builder)
	m.writePluginDetail(&builder)
	m.writePluginMenu(&builder)
	return builder.String()
}

func (m *model) writePluginNotices(builder *strings.Builder) {
	if m.pluginsNotice != "" {
		builder.WriteString("\n" + m.st.dim.Render("  "+m.pluginsNotice))
	}
	if len(m.pluginsRows) == 0 && m.pluginsNotice == "" {
		builder.WriteString("\n" + m.st.dim.Render("  (aucun plugin compilé dans ce binaire)"))
	}
}

func (m *model) writePluginRows(builder *strings.Builder) {
	start, end := pluginsWindow(m.pluginsIdx, len(m.pluginsRows))
	if start > 0 {
		builder.WriteString("\n" + m.st.dim.Render(fmt.Sprintf("  … %d au-dessus", start)))
	}
	for i := start; i < end; i++ {
		builder.WriteString("\n" + m.pluginRow(i))
	}
	if end < len(m.pluginsRows) {
		builder.WriteString("\n" + m.st.dim.Render(fmt.Sprintf("  … %d en dessous", len(m.pluginsRows)-end)))
	}
}

func (m *model) pluginRow(index int) string {
	pluginRow := m.pluginsRows[index]
	row := fmt.Sprintf("%s · %s → %s", pluginRow.Module, pluginRow.Installed, pluginRow.Latest)
	if pluginRow.Pinned {
		row += " · épinglé"
	}
	if index == m.pluginsIdx && m.pluginsMode == pluginsList {
		return m.st.accent.Render(m.st.glyphs.cursor) + " " + m.st.text.Render(row)
	}
	return m.st.dim.Render("  " + row)
}

func (m *model) writePluginDetail(builder *strings.Builder) {
	if m.pluginsMode == pluginsWarn {
		for _, f := range m.pluginsFindings {
			builder.WriteString("\n" + m.st.text.Render("  ! "+f))
		}
	}
	if m.pluginsMode == pluginsFailed && m.pluginsOutput != "" {
		for _, line := range strings.Split(strings.TrimRight(m.pluginsOutput, "\n"), "\n") {
			builder.WriteString("\n" + m.st.dim.Render("  "+line))
		}
	}
}

func (m *model) writePluginMenu(builder *strings.Builder) {
	if m.pluginsMode == pluginsList || m.pluginsMode == pluginsDone {
		return
	}
	for i, opt := range m.pluginsMenu {
		if i == m.pluginsMenuIdx {
			builder.WriteString("\n" + m.st.accent.Render(m.st.glyphs.cursor) + " " + m.st.text.Render(opt))
		} else {
			builder.WriteString("\n" + m.st.dim.Render("  "+opt))
		}
	}
}
