package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	terminalConfigFile  = "terminal.toml"
	terminalSectionName = "terminal"
	themesDirName       = "themes"
	defaultThemeID      = themeSombre
	themeFileExt        = ".toml"
	keyTheme            = "theme"
)

var (
	errThemeCycle    = errors.New("theme base cycle")
	errUnknownKey    = errors.New("unknown key")
	errInvalidColor  = errors.New("invalid colour")
	errUnknownBase   = errors.New("unknown base")
	errUnknownOption = errors.New("unknown value")
)

type themeFile struct {
	Name   string     `toml:"name"`
	Base   string     `toml:"base"`
	Colors colorsFile `toml:"colors"`
	Render renderFile `toml:"render"`
}

type colorsFile struct {
	Text    *string `toml:"text"`
	Dim     *string `toml:"dim"`
	Accent  *string `toml:"accent"`
	Add     *string `toml:"add"`
	Del     *string `toml:"del"`
	Wait    *string `toml:"wait"`
	Err     *string `toml:"err"`
	Sel     *string `toml:"sel"`
	SelText *string `toml:"sel_text"`
	Violet  *string `toml:"violet"`
	AddBg   *string `toml:"add_bg"`
	DelBg   *string `toml:"del_bg"`
}

type renderFile struct {
	Glyphs  *string `toml:"glyphs"`
	Borders *string `toml:"borders"`
	Density *string `toml:"density"`
	Bold    *bool   `toml:"bold"`
	Italic  *bool   `toml:"italic"`
}

func readThemeFile(path string) (themeFile, error) {
	var file themeFile
	meta, err := toml.DecodeFile(path, &file)
	if err != nil {
		return themeFile{}, err
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return themeFile{}, fmt.Errorf("%w %s", errUnknownKey, undecoded[0])
	}
	return file, nil
}

func (f themeFile) overlay(base Theme, themeName string) (Theme, error) {
	resolved := base
	resolved.ID = themeName
	resolved.Label = themeName
	if f.Name != "" {
		resolved.Label = f.Name
	}
	if err := f.Colors.applyTo(&resolved.Colors); err != nil {
		return Theme{}, err
	}
	look, err := f.Render.applyTo(base.Render)
	if err != nil {
		return Theme{}, err
	}
	resolved.Render = look
	return resolved, nil
}

func invalidColorReason(value string) error {
	return detailedReason{
		sentinel: errInvalidColor,
		text:     fmt.Sprintf("%s %q (expected #rrggbb or ansi:0 to ansi:15)", errInvalidColor, value),
		render: func(trans translator) string {
			return trans.t("tui.reason.invalid_color_value", map[string]any{tplArgValue: value})
		},
	}
}

func (c colorsFile) applyTo(pal *palette) error {
	overrides := []struct {
		dst *colorValue
		src *string
	}{
		{&pal.Text, c.Text},
		{&pal.Dim, c.Dim},
		{&pal.Accent, c.Accent},
		{&pal.Add, c.Add},
		{&pal.Del, c.Del},
		{&pal.Wait, c.Wait},
		{&pal.Err, c.Err},
		{&pal.Sel, c.Sel},
		{&pal.SelText, c.SelText},
		{&pal.Violet, c.Violet},
		{&pal.AddBg, c.AddBg},
		{&pal.DelBg, c.DelBg},
	}
	for _, override := range overrides {
		if override.src == nil {
			continue
		}
		if !validColor(*override.src) {
			return invalidColorReason(*override.src)
		}
		*override.dst = colorValue(*override.src)
	}
	return nil
}

func (f renderFile) applyTo(base renderOptions) (renderOptions, error) {
	look := base
	if f.Glyphs != nil {
		mode, err := parseOption("glyphs", *f.Glyphs, glyphModes())
		if err != nil {
			return renderOptions{}, err
		}
		look.Glyphs = mode
	}
	if f.Borders != nil {
		style, err := parseOption("borders", *f.Borders, borderStyles())
		if err != nil {
			return renderOptions{}, err
		}
		look.Borders = style
	}
	if f.Density != nil {
		level, err := parseOption("density", *f.Density, densities())
		if err != nil {
			return renderOptions{}, err
		}
		look.Density = level
	}
	if f.Bold != nil {
		look.Bold = *f.Bold
	}
	if f.Italic != nil {
		look.Italic = *f.Italic
	}
	return look, nil
}

type themeCatalog struct {
	themes   []Theme
	paths    map[string]string
	problems []themeProblem
}

type themeProblem struct {
	name    string
	reason  error
	builtin bool
}

func (problem themeProblem) text(trans translator) string {
	if problem.builtin {
		return trans.t("tui.theme.problem.builtin", map[string]any{tplArgName: problem.name})
	}
	return trans.t("tui.theme.problem.rejected", map[string]any{tplArgName: problem.name, tplArgReason: localizedReason(trans, problem.reason)})
}

func loadThemeCatalog(dir string) themeCatalog {
	catalog := themeCatalog{themes: builtinThemes(), paths: make(map[string]string)}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return catalog
	}
	raws := make(map[string]themeFile)
	var names []string
	for _, entry := range entries {
		fileName := entry.Name()
		if entry.IsDir() || filepath.Ext(fileName) != themeFileExt {
			continue
		}
		themeName := strings.TrimSuffix(fileName, themeFileExt)
		if _, builtin := builtinTheme(themeName); builtin {
			catalog.problems = append(catalog.problems, themeProblem{name: fileName, builtin: true})
			continue
		}
		path := filepath.Join(dir, fileName)
		file, err := readThemeFile(path)
		if err != nil {
			catalog.problems = append(catalog.problems, themeProblem{name: fileName, reason: err})
			continue
		}
		raws[themeName] = file
		catalog.paths[themeName] = path
		names = append(names, themeName)
	}
	sort.Strings(names)
	for _, themeName := range names {
		resolved, err := resolveTheme(themeName, raws, make(map[string]bool))
		if err != nil {
			catalog.problems = append(catalog.problems, themeProblem{name: themeName, reason: err})
			delete(catalog.paths, themeName)
			continue
		}
		catalog.themes = append(catalog.themes, resolved)
	}
	return catalog
}

func resolveTheme(themeName string, raws map[string]themeFile, seen map[string]bool) (Theme, error) {
	if seen[themeName] {
		return Theme{}, errThemeCycle
	}
	seen[themeName] = true
	file := raws[themeName]
	baseName := file.Base
	if baseName == "" {
		baseName = defaultThemeID
	}
	base, builtin := builtinTheme(baseName)
	if !builtin {
		if _, custom := raws[baseName]; !custom {
			return Theme{}, fmt.Errorf("%w %q", errUnknownBase, baseName)
		}
		parent, err := resolveTheme(baseName, raws, seen)
		if err != nil {
			return Theme{}, err
		}
		base = parent
	}
	return file.overlay(base, themeName)
}

func (c themeCatalog) find(themeName string) (Theme, bool) {
	for index := range c.themes {
		if c.themes[index].ID == themeName {
			return c.themes[index], true
		}
	}
	return Theme{}, false
}

func (c themeCatalog) indexOf(themeName string) int {
	for index := range c.themes {
		if c.themes[index].ID == themeName {
			return index
		}
	}
	return -1
}

type terminalSection struct {
	Theme     *string `toml:"theme,omitempty"`
	AutoTheme *bool   `toml:"auto_theme,omitempty"`
	Glyphs    *string `toml:"glyphs,omitempty"`
	Borders   *string `toml:"borders,omitempty"`
	Density   *string `toml:"density,omitempty"`
	Bold      *bool   `toml:"bold,omitempty"`
	Italic    *bool   `toml:"italic,omitempty"`
}

type terminalFile struct {
	Terminal terminalSection `toml:"terminal"`
}

func readTerminalConfig(path string) (terminalSection, error) {
	var file terminalFile
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return terminalSection{}, nil
	}
	if _, err := toml.DecodeFile(path, &file); err != nil {
		return terminalSection{}, err
	}
	return file.Terminal, nil
}

func themeKeyValues(section terminalSection) []tomlKeyValue {
	return []tomlKeyValue{
		{key: keyTheme, value: quotedOrEmpty(section.Theme)},
		{key: "auto_theme", value: boolOrEmpty(section.AutoTheme)},
		{key: "glyphs", value: quotedOrEmpty(section.Glyphs)},
		{key: "borders", value: quotedOrEmpty(section.Borders)},
		{key: "density", value: quotedOrEmpty(section.Density)},
		{key: "bold", value: boolOrEmpty(section.Bold)},
		{key: "italic", value: boolOrEmpty(section.Italic)},
	}
}

func quotedOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return strconv.Quote(*value)
}

func boolOrEmpty(value *bool) string {
	if value == nil {
		return ""
	}
	return strconv.FormatBool(*value)
}

func (s terminalSection) themeID() string {
	if s.Theme == nil || *s.Theme == "" {
		return defaultThemeID
	}
	return *s.Theme
}

func (s terminalSection) autoTheme() bool {
	return s.AutoTheme != nil && *s.AutoTheme
}

func (s terminalSection) renderFile() renderFile {
	return renderFile{Glyphs: s.Glyphs, Borders: s.Borders, Density: s.Density, Bold: s.Bold, Italic: s.Italic}
}

func (s terminalSection) look(base renderOptions) (renderOptions, error) {
	return s.renderFile().applyTo(base)
}

func (s terminalSection) applyLook(current Theme) Theme {
	look, err := s.look(current.Render)
	if err != nil {
		return current
	}
	current.Render = look
	return current
}

func themesDirFor(configDir string) string {
	return filepath.Join(configDir, themesDirName)
}

func themesStamp(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != themeFileExt {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%d", entry.Name(), info.ModTime().UnixNano(), info.Size()))
	}
	return strings.Join(parts, "|")
}
