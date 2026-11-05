package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/skills"
)

func skillPanelRoots() []string {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, ".claude", "skills"))
	}
	if cfg, err := config.Load(config.DefaultPath()); err == nil && cfg.Skills != nil {
		roots = append(roots, cfg.Skills.Roots...)
	}
	return roots
}

func (m *model) openSkills() {
	rows := skills.Discover(skillPanelRoots())
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	m.skillsRows = rows
	m.skillsIdx = 0
	m.skillsOpen = true
}

func (m *model) clampSkills() {
	count := len(m.skillsRows)
	if m.skillsIdx >= count {
		m.skillsIdx = count - 1
	}
	if m.skillsIdx < 0 {
		m.skillsIdx = 0
	}
}

func (m *model) moveSkills(d int) { m.skillsIdx += d; m.clampSkills() }

func (m *model) skillsView() string {
	if len(m.skillsRows) == 0 {
		return m.st.dim.Render("  (aucune skill dans ~/.claude/skills)")
	}
	start, end := paletteWindow(len(m.skillsRows), m.skillsIdx, m.overlayRows())
	rows := make([]overlayRow, 0, end-start)
	for i := start; i < end; i++ {
		s := m.skillsRows[i]
		rows = append(rows, overlayRow{label: s.Name, detail: s.Description})
	}
	footer := joinNonEmpty(
		m.pairHint(actSelectPrevious, actSelectNext, "parcourir", ctxSkills),
		m.pairHint(actSelectPageUp, actSelectPageDown, "page", ctxSkills),
		m.labelledHint(actSelectCancel, "fermer", ctxSkills),
	)
	if hidden := len(m.skillsRows) - (end - start); hidden > 0 {
		footer = fmt.Sprintf("+%d autres · %s", hidden, footer)
	}
	return floatingList(rows, m.skillsIdx-start, footer, m.overlayWidth(), m.st)
}
