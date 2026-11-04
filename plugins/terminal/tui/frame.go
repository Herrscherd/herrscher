package tui

import (
	"strings"
	"time"
)

const (
	minTranscriptRows = 2
	unboundedRows     = 1 << 30
)

type frame struct {
	above     []string
	below     []string
	stripRow  int
	stripDown bool
}

func (f frame) height() int {
	return len(f.above) + len(f.below)
}

func (f frame) render(viewport string) string {
	parts := make([]string, 0, f.height()+1)
	parts = append(parts, f.above...)
	parts = append(parts, viewport)
	parts = append(parts, f.below...)
	return strings.Join(parts, "\n")
}

func (m *model) buildFrame() frame {
	m.input.Placeholder = m.placeholderText()
	m.syncPrompt()
	panels, room := m.frameBudget()
	return m.composeFrame(panels, room, true)
}

func (m *model) frameBudget() (bool, int) {
	if m.height <= 0 {
		return true, unboundedRows
	}
	fixed := m.composeFrame(false, 0, false).height()
	natural := m.composeFrame(false, m.height, false).height() - fixed
	avail := m.height - fixed - minTranscriptRows
	panels := len(m.sidePanelLines())
	if panels <= avail-natural {
		return true, avail - panels
	}
	return false, avail
}

func (m *model) composeFrame(panels bool, room int, flexible bool) frame {
	stacked := frame{above: m.railLines()}
	if panels {
		stacked.below = append(stacked.below, m.sidePanelLines()...)
	}
	surface := m.approvalLines(time.Now(), room)
	if m.planMenu != nil {
		surface = appendLines(surface, m.planMenuView())
	}
	surface = fitBlock(surface, room)
	stacked.below = append(stacked.below, surface...)
	m.appendModeStrip(&stacked)
	chips := appendLines(nil, m.chipsView())
	drafts := appendLines(nil, m.draftsView())
	composer := m.composerZone()
	status := m.statusZone()
	fixedRows := len(stacked.above) + len(stacked.below) + len(chips) + len(drafts) + len(composer) + len(status)
	var overlays []string
	if flexible {
		overlays = fitBlock(m.openOverlayLines(), m.rowsLeft(fixedRows+m.queueMinRows()))
		stacked.below = append(stacked.below, overlays...)
	}
	stacked.below = append(stacked.below, chips...)
	if flexible {
		base := len(stacked.above) + m.vp.Height + len(stacked.below)
		queue := m.queueLines(base, m.rowsLeft(fixedRows+len(overlays)))
		stacked.below = appendLines(stacked.below, strings.Join(queue, "\n"))
	}
	stacked.below = append(stacked.below, drafts...)
	stacked.below = append(stacked.below, composer...)
	stacked.below = append(stacked.below, status...)
	return stacked
}

func (m *model) openOverlayLines() []string {
	var overlays []string
	for _, overlay := range m.frameOverlays() {
		if overlay.open {
			overlays = appendLines(overlays, overlay.view())
		}
	}
	return overlays
}

func (m *model) chipsView() string {
	chips := chipRow(m.pending, m.st)
	if chips == "" {
		return ""
	}
	return chips + "  " + m.st.dim.Render("⌃U retirer")
}

func (m *model) rowsLeft(reserved int) int {
	if m.height <= 0 {
		return unboundedRows
	}
	return m.height - minTranscriptRows - reserved
}

func (m *model) queueMinRows() int {
	if current := m.activeTab(); current != nil && len(current.queue) > 0 {
		return 1
	}
	return 0
}

func (m *model) sidePanelLines() []string {
	tab := m.tabs[m.active]
	if tab == nil || m.sidebarVisible() {
		return nil
	}
	lines := todoPanel(tab.todos, m.innerWidth(), m.st)
	return append(lines, subagentPanel(tab.agents, time.Now(), m.innerWidth(), m.st)...)
}

func fitBlock(lines []string, room int) []string {
	if len(lines) <= room {
		return lines
	}
	if room <= 0 {
		return nil
	}
	if room == 1 {
		return lines[:1]
	}
	return append(append([]string{}, lines[:room-1]...), lines[len(lines)-1])
}

func (m *model) appendModeStrip(stacked *frame) {
	tab := m.tabs[m.active]
	if tab == nil {
		return
	}
	stacked.stripRow = len(stacked.above) + m.vp.Height + len(stacked.below)
	stacked.stripDown = true
	stacked.below = append(stacked.below, m.modeStripView(tab, m.innerWidth())...)
}

type frameOverlay struct {
	open bool
	view func() string
}

func (m *model) frameOverlays() []frameOverlay {
	return []frameOverlay{
		{m.paletteOpen(), m.paletteView},
		{m.mentionOpen(), m.mentionView},
		{m.resumeOpen, m.resumeView},
		{m.rwOpen, m.rewindView},
		{m.switchOpen, m.switchView},
		{m.skillsOpen, m.skillsView},
		{m.diagOpen, m.diagView},
		{m.ctxOpen, m.contextView},
		{m.pluginsOpen, m.pluginsView},
		{m.searchOpen, m.searchView},
		{m.showHelp, m.helpView},
		{m.bar.open, m.statuslineView},
		{m.histOpen, m.histView},
	}
}

func appendLines(dst []string, block string) []string {
	if block == "" {
		return dst
	}
	return append(dst, strings.Split(block, "\n")...)
}
