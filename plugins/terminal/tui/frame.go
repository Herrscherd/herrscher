package tui

import (
	"strings"
	"time"
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
	stacked := frame{above: m.railLines()}
	m.appendSidePanels(&stacked)
	stacked.below = append(stacked.below, m.approvalLines(time.Now())...)
	if m.planMenu != nil {
		stacked.below = appendLines(stacked.below, m.planMenuView())
	}
	m.appendModeStrip(&stacked)
	for _, overlay := range m.frameOverlays() {
		if overlay.open {
			stacked.below = appendLines(stacked.below, overlay.view())
		}
	}
	if chips := chipRow(m.pending, m.st); chips != "" {
		stacked.below = appendLines(stacked.below, chips+"  "+m.st.dim.Render("⌃U retirer"))
	}
	base := len(stacked.above) + m.vp.Height + len(stacked.below)
	stacked.below = appendLines(stacked.below, strings.Join(m.queueLines(base), "\n"))
	stacked.below = appendLines(stacked.below, m.draftsView())
	stacked.below = append(stacked.below, m.composerZone()...)
	stacked.below = append(stacked.below, m.statusZone()...)
	return stacked
}

func (m *model) appendSidePanels(stacked *frame) {
	tab := m.tabs[m.active]
	if tab == nil || m.sidebarVisible() {
		return
	}
	stacked.below = append(stacked.below, todoPanel(tab.todos, m.innerWidth(), m.st)...)
	stacked.below = append(stacked.below, subagentPanel(tab.agents, time.Now(), m.innerWidth(), m.st)...)
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
		{m.themeScr != nil, m.themeView},
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
