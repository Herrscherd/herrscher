package tui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	terminalConfigFile = "terminal.toml"
	themesDirName      = "themes"
	defaultThemeID     = "sombre"
	themeFileExt       = ".toml"
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

var errThemeCycle = errors.New("base de thème circulaire")

func readThemeFile(path string) (themeFile, error) {
	var file themeFile
	meta, err := toml.DecodeFile(path, &file)
	if err != nil {
		return themeFile{}, err
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return themeFile{}, fmt.Errorf("clé inconnue %s", undecoded[0])
	}
	return file, nil
}

func (f themeFile) overlay(base Theme, id string) (Theme, error) {
	th := base
	th.ID = id
	th.Label = id
	if f.Name != "" {
		th.Label = f.Name
	}
	colors := []struct {
		dst *colorValue
		src *string
	}{
		{&th.Colors.Text, f.Colors.Text}, {&th.Colors.Dim, f.Colors.Dim}, {&th.Colors.Accent, f.Colors.Accent},
		{&th.Colors.Add, f.Colors.Add}, {&th.Colors.Del, f.Colors.Del}, {&th.Colors.Wait, f.Colors.Wait},
		{&th.Colors.Err, f.Colors.Err}, {&th.Colors.Sel, f.Colors.Sel}, {&th.Colors.SelText, f.Colors.SelText},
		{&th.Colors.Violet, f.Colors.Violet}, {&th.Colors.AddBg, f.Colors.AddBg}, {&th.Colors.DelBg, f.Colors.DelBg},
	}
	for _, color := range colors {
		if color.src == nil {
			continue
		}
		if !validColor(*color.src) {
			return Theme{}, fmt.Errorf("couleur invalide %q (attendu #rrggbb ou ansi:0 à ansi:15)", *color.src)
		}
		*color.dst = colorValue(*color.src)
	}
	if f.Render.Glyphs != nil {
		th.Render.Glyphs = *f.Render.Glyphs
	}
	if f.Render.Borders != nil {
		th.Render.Borders = *f.Render.Borders
	}
	if f.Render.Density != nil {
		th.Render.Density = *f.Render.Density
	}
	if f.Render.Bold != nil {
		th.Render.Bold = *f.Render.Bold
	}
	if f.Render.Italic != nil {
		th.Render.Italic = *f.Render.Italic
	}
	if err := th.Render.validate(); err != nil {
		return Theme{}, err
	}
	return th, nil
}

func (r renderOptions) validate() error {
	if !oneOf(r.Glyphs, glyphsUnicode, glyphsNerd, glyphsASCII) {
		return fmt.Errorf("glyphs inconnu %q (unicode, nerd ou ascii)", r.Glyphs)
	}
	if !oneOf(r.Borders, bordersRounded, bordersSquare, bordersNone) {
		return fmt.Errorf("borders inconnu %q (rounded, square ou none)", r.Borders)
	}
	if !oneOf(r.Density, densityNormal, densityCompact) {
		return fmt.Errorf("density inconnu %q (normal ou compact)", r.Density)
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

type themeCatalog struct {
	themes   []Theme
	paths    map[string]string
	problems []string
}

func loadThemeCatalog(dir string) themeCatalog {
	catalog := themeCatalog{themes: builtinThemes(), paths: map[string]string{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return catalog
	}
	raws := map[string]themeFile{}
	var ids []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != themeFileExt {
			continue
		}
		id := strings.TrimSuffix(name, themeFileExt)
		if _, builtin := builtinTheme(id); builtin {
			catalog.problems = append(catalog.problems, fmt.Sprintf("thème %s ignoré : ce nom est intégré", name))
			continue
		}
		path := filepath.Join(dir, name)
		file, err := readThemeFile(path)
		if err != nil {
			catalog.problems = append(catalog.problems, fmt.Sprintf("thème %s ignoré : %v", name, err))
			continue
		}
		raws[id] = file
		catalog.paths[id] = path
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		th, err := resolveTheme(id, raws, map[string]bool{})
		if err != nil {
			catalog.problems = append(catalog.problems, fmt.Sprintf("thème %s ignoré : %v", id, err))
			delete(catalog.paths, id)
			continue
		}
		catalog.themes = append(catalog.themes, th)
	}
	return catalog
}

func resolveTheme(id string, raws map[string]themeFile, seen map[string]bool) (Theme, error) {
	if seen[id] {
		return Theme{}, errThemeCycle
	}
	seen[id] = true
	file := raws[id]
	baseID := file.Base
	if baseID == "" {
		baseID = defaultThemeID
	}
	base, builtin := builtinTheme(baseID)
	if !builtin {
		if _, custom := raws[baseID]; !custom {
			return Theme{}, fmt.Errorf("base inconnue %q", baseID)
		}
		parent, err := resolveTheme(baseID, raws, seen)
		if err != nil {
			return Theme{}, err
		}
		base = parent
	}
	return file.overlay(base, id)
}

func (c themeCatalog) find(id string) (Theme, bool) {
	for _, th := range c.themes {
		if th.ID == id {
			return th, true
		}
	}
	return Theme{}, false
}

func (c themeCatalog) indexOf(id string) int {
	for i, th := range c.themes {
		if th.ID == id {
			return i
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

func writeTerminalConfig(path string, section terminalSection) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	file, err := os.Create(tmp)
	if err != nil {
		return err
	}
	encodeErr := toml.NewEncoder(file).Encode(terminalFile{Terminal: section})
	closeErr := file.Close()
	if err := errors.Join(encodeErr, closeErr); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
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

func (s terminalSection) applyLook(th Theme) Theme {
	if s.Glyphs != nil {
		th.Render.Glyphs = *s.Glyphs
	}
	if s.Borders != nil {
		th.Render.Borders = *s.Borders
	}
	if s.Density != nil {
		th.Render.Density = *s.Density
	}
	if s.Bold != nil {
		th.Render.Bold = *s.Bold
	}
	if s.Italic != nil {
		th.Render.Italic = *s.Italic
	}
	return th
}

func (s terminalSection) validate() error {
	look := renderOptions{Glyphs: glyphsUnicode, Borders: bordersRounded, Density: densityNormal}
	if s.Glyphs != nil {
		look.Glyphs = *s.Glyphs
	}
	if s.Borders != nil {
		look.Borders = *s.Borders
	}
	if s.Density != nil {
		look.Density = *s.Density
	}
	return look.validate()
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
