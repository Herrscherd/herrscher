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
	id := cfg.themeID()
	if cfg.autoTheme() && (id == "sombre" || id == "clair") {
		id = "clair"
		if detectDark() {
			id = "sombre"
		}
	}
	th, ok := catalog.find(id)
	if !ok {
		th = defaultTheme()
	}
	return cfg.applyLook(th)
}

func (m *model) loadTheme(configDir string, detectDark func() bool) {
	m.themeDir = configDir
	m.noColor = os.Getenv("NO_COLOR") != ""
	cfg, err := readTerminalConfig(filepath.Join(configDir, terminalConfigFile))
	if err != nil {
		m.flash = "terminal.toml ignoré : " + err.Error()
		cfg = terminalSection{}
	}
	if err := cfg.validate(); err != nil {
		m.flash = "terminal.toml : " + err.Error()
		cfg.Glyphs, cfg.Borders, cfg.Density = nil, nil, nil
	}
	m.themeCfg = cfg
	m.themeCat = loadThemeCatalog(themesDirFor(configDir))
	m.themeStamp = themesStamp(themesDirFor(configDir))
	m.applyTheme(startupTheme(cfg, m.themeCat, detectDark))
}

func (m *model) applyTheme(th Theme) {
	m.theme = th
	m.st = newStyles(th, m.noColor)
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
	th, ok := m.themeCat.find(m.theme.ID)
	if !ok {
		th = defaultTheme()
		m.flash = fmt.Sprintf("thème %s introuvable : retour au thème %s", m.theme.ID, th.ID)
	}
	m.applyTheme(m.themeCfg.applyLook(th))
}

func (m *model) saveThemeConfig(section terminalSection) error {
	path := filepath.Join(m.themeDir, terminalConfigFile)
	if err := updateTomlFile(path, terminalSectionName, themeKeyValues(section)...); err != nil {
		return err
	}
	m.themeCfg = section
	return nil
}
