package tui

import (
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
		m.flash = m.tr.t("tui.theme.toml_ignored", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		cfg = terminalSection{}
	}
	if _, err := cfg.look(defaultRenderOptions()); err != nil {
		m.flash = m.tr.t("tui.theme.toml_invalid", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		cfg.Glyphs, cfg.Borders, cfg.Density = nil, nil, nil
	}
	m.themeCfg = cfg
	m.loadKeys(configDir)
	m.themeCat = loadThemeCatalog(themesDirFor(configDir))
	m.themeStamp = themesStamp(themesDirFor(configDir))
	m.themeBackground = ""
	probe := func() bool {
		dark := detectDark()
		m.themeBackground = backgroundCode(dark)
		return dark
	}
	m.applyTheme(startupTheme(cfg, m.themeCat, probe))
}

func backgroundCode(dark bool) string {
	if dark {
		return backgroundDark
	}
	return backgroundLight
}

const (
	backgroundDark  = "dark"
	backgroundLight = "light"
)

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
		m.flash = m.tr.t("tui.theme.missing", map[string]any{tplArgName: m.theme.ID, "Fallback": found.ID})
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
