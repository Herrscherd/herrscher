package tui

import "strings"

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
	f := frame{above: []string{m.bannerRow()}}
	if m.choice != nil {
		f.below = appendLines(f.below, m.choiceView())
	}
	if m.paletteOpen() {
		f.below = appendLines(f.below, m.paletteView())
	}
	if m.mentionOpen() {
		f.below = appendLines(f.below, m.mentionView())
	}
	if m.resumeOpen {
		f.below = appendLines(f.below, m.resumeView())
	}
	if m.switchOpen {
		f.below = appendLines(f.below, m.switchView())
	}
	if m.skillsOpen {
		f.below = appendLines(f.below, m.skillsView())
	}
	if m.diagOpen {
		f.below = appendLines(f.below, m.diagView())
	}
	if m.pluginsOpen {
		f.below = appendLines(f.below, m.pluginsView())
	}
	if m.searchOpen {
		f.below = appendLines(f.below, m.searchView())
	}
	if m.showHelp {
		f.below = appendLines(f.below, m.helpView())
	}
	if chips := chipRow(m.pending); chips != "" {
		f.below = appendLines(f.below, chips+"  "+dimStyle.Render("⌃U remove"))
	}
	f.below = appendLines(f.below, m.separatorRow())
	f.below = appendLines(f.below, m.statusFooter())
	f.below = appendLines(f.below, m.inputRow())
	return f
}

func appendLines(dst []string, block string) []string {
	if block == "" {
		return dst
	}
	return append(dst, strings.Split(block, "\n")...)
}
