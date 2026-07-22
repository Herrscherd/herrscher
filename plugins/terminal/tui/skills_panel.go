package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/skills"
)

// skillsMax bounds how many rows the /skills panel shows at once.
const skillsMax = 10

// skillPanelRoots is the read-only search path the /skills panel lists from: the
// user-global ~/.claude/skills, then any extra roots declared in config. The
// panel is session-agnostic (the TUI is one gateway over many sessions), so it
// deliberately omits per-session workspace roots.
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

// openSkills discovers the available skills and opens the read-only panel,
// sorted by name for a stable listing.
func (m *model) openSkills() {
	rows := skills.Discover(skillPanelRoots())
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	m.skillsRows = rows
	m.skillsIdx = 0
	m.skillsOpen = true
}

// clampSkills keeps the selection index within the current row set.
func (m *model) clampSkills() {
	n := len(m.skillsRows)
	if m.skillsIdx >= n {
		m.skillsIdx = n - 1
	}
	if m.skillsIdx < 0 {
		m.skillsIdx = 0
	}
}

func (m *model) moveSkills(d int) { m.skillsIdx += d; m.clampSkills() }

// skillsView renders the panel as an inline Claude menu (no border box): a dim
// header, then one row per skill with its name and description, the selected row
// prefixed ❯ in the warm accent.
func (m *model) skillsView() string {
	var b strings.Builder
	b.WriteString(dimStyle.Render("skills — ↑↓ select · Esc close"))
	if len(m.skillsRows) == 0 {
		b.WriteString("\n" + dimStyle.Render("  (no skills found in ~/.claude/skills)"))
		return b.String()
	}
	for i, s := range m.skillsRows {
		if i >= skillsMax {
			b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  … +%d more", len(m.skillsRows)-skillsMax)))
			break
		}
		row := s.Name
		if s.Description != "" {
			row += " — " + s.Description
		}
		if i == m.skillsIdx {
			b.WriteString("\n" + warmStyle.Render(glyphCursor+" "+row))
		} else {
			b.WriteString("\n" + dimStyle.Render("  "+row))
		}
	}
	return b.String()
}

// skillsHeight is the rendered row count of the open panel (0 when closed), so
// chromeHeight can reserve space for it.
func (m *model) skillsHeight() int {
	if !m.skillsOpen {
		return 0
	}
	return lipgloss.Height(m.skillsView())
}
