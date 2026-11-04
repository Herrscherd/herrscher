package tui

import (
	"strings"
	"time"
)

type frame struct {
	above []string
	below []string
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
	stacked := frame{above: []string{m.railRow()}}
	if tab := m.tabs[m.active]; tab != nil {
		stacked.below = append(stacked.below, todoPanel(tab.todos, m.innerWidth(), m.st)...)
		stacked.below = append(stacked.below, subagentPanel(tab.agents, time.Now(), m.innerWidth(), m.st)...)
	}
	stacked.below = append(stacked.below, m.approvalLines(time.Now())...)
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
	if chips := chipRow(m.pending, m.st); chips != "" {
		stacked.below = appendLines(stacked.below, chips+"  "+m.st.dim.Render("⌃U retirer"))
	}
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
