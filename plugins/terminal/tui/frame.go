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
	stacked := frame{above: m.railLines()}
	if tab := m.tabs[m.active]; tab != nil && !m.sidebarVisible() {
		stacked.below = append(stacked.below, todoPanel(tab.todos, m.innerWidth(), m.st)...)
		stacked.below = append(stacked.below, subagentPanel(tab.agents, time.Now(), m.innerWidth(), m.st)...)
	}
	stacked.below = append(stacked.below, m.approvalLines(time.Now())...)
	if m.planMenu != nil {
		stacked.below = appendLines(stacked.below, m.planMenuView())
	}
	if tab := m.tabs[m.active]; tab != nil {
		stacked.stripRow = len(stacked.above) + m.vp.Height + len(stacked.below)
		stacked.stripDown = true
		stacked.below = append(stacked.below, m.modeStripView(tab, m.innerWidth())...)
	}
	if m.paletteOpen() {
		stacked.below = appendLines(stacked.below, m.paletteView())
	}
	if m.mentionOpen() {
		stacked.below = appendLines(stacked.below, m.mentionView())
	}
	if m.resumeOpen {
		stacked.below = appendLines(stacked.below, m.resumeView())
	}
	if m.switchOpen {
		stacked.below = appendLines(stacked.below, m.switchView())
	}
	if m.themeScr != nil {
		stacked.below = appendLines(stacked.below, m.themeView())
	}
	if m.skillsOpen {
		stacked.below = appendLines(stacked.below, m.skillsView())
	}
	if m.diagOpen {
		stacked.below = appendLines(stacked.below, m.diagView())
	}
	if m.pluginsOpen {
		stacked.below = appendLines(stacked.below, m.pluginsView())
	}
	if m.searchOpen {
		stacked.below = appendLines(stacked.below, m.searchView())
	}
	if m.showHelp {
		stacked.below = appendLines(stacked.below, m.helpView())
	}
	if m.bar.open {
		stacked.below = appendLines(stacked.below, m.statuslineView())
	}
	if m.histOpen {
		stacked.below = appendLines(stacked.below, m.histView())
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

func appendLines(dst []string, block string) []string {
	if block == "" {
		return dst
	}
	return append(dst, strings.Split(block, "\n")...)
}
