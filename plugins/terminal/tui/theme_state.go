package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const themeWatchInterval = time.Second

type themeWatchMsg struct{}

func themeWatchTick() tea.Cmd {
	return tea.Tick(themeWatchInterval, func(time.Time) tea.Msg { return themeWatchMsg{} })
}

func defaultTheme() Theme {
	th, _ := builtinTheme(defaultThemeID)
	return th
}

func startupTheme(cfg terminalSection, catalog themeCatalog, detectDark func() bool) Theme {
	themeName := cfg.themeID()
	if cfg.autoTheme() && (themeName == themeSombre || themeName == themeClair) {
		themeName = themeClair
		if detectDark() {
			themeName = themeSombre
		}
	}
	found, ok := catalog.find(themeName)
	if !ok {
		found = defaultTheme()
	}
	return cfg.applyLook(found)
}

func (m *model) terminalConfigPath() string {
	return filepath.Join(m.themeDir, terminalConfigFile)
}

func (m *model) loadTheme(configDir string, detectDark func() bool) {
	m.themeDir = configDir
	m.noColor = os.Getenv("NO_COLOR") != ""
	cfg, err := readTerminalConfig(m.terminalConfigPath())
	if err != nil {
		m.flash = "terminal.toml ignoré : " + err.Error()
		cfg = terminalSection{}
	}
	if _, err := cfg.look(defaultRenderOptions()); err != nil {
		m.flash = "terminal.toml : " + err.Error()
		cfg.Glyphs, cfg.Borders, cfg.Density = nil, nil, nil
	}
	m.themeCfg = cfg
	m.loadKeys(configDir)
	m.themeCat = loadThemeCatalog(themesDirFor(configDir))
	m.themeStamp = themesStamp(themesDirFor(configDir))
	m.themeBackground = ""
	probe := func() bool {
		dark := detectDark()
		m.themeBackground = backgroundLabel(dark)
		return dark
	}
	m.applyTheme(startupTheme(cfg, m.themeCat, probe))
}

func backgroundLabel(dark bool) string {
	if dark {
		return "sombre"
	}
	return "clair"
}

func (m *model) applyTheme(next Theme) {
	m.theme = next
	m.st = newStyles(next, m.noColor)
	m.md = &markdownRenderer{}
	m.tsCache.valid = false
}

func (m *model) watchThemes() {
	if m.themeDir == "" {
		return
	}
	stamp := themesStamp(themesDirFor(m.themeDir))
	if stamp == m.themeStamp {
		return
	}
	m.themeStamp = stamp
	m.reloadThemes()
}

func (m *model) reloadThemes() {
	m.themeCat = loadThemeCatalog(themesDirFor(m.themeDir))
	if m.themeScr != nil {
		m.themeScr.refresh(m.themeCat)
		m.applyTheme(m.themeScr.preview())
		return
	}
	found, ok := m.themeCat.find(m.theme.ID)
	if !ok {
		found = defaultTheme()
		m.flash = fmt.Sprintf("thème %s introuvable : retour au thème %s", m.theme.ID, found.ID)
	}
	m.applyTheme(m.themeCfg.applyLook(found))
}

func (m *model) saveThemeConfig(section terminalSection) error {
	if err := updateTomlFile(m.terminalConfigPath(), terminalSectionName, themeKeyValues(section)...); err != nil {
		return err
	}
	m.themeCfg = section
	return nil
}
