package tui

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const (
	glyphsUnicode = "unicode"
	glyphsNerd    = "nerd"
	glyphsASCII   = "ascii"

	bordersRounded = "rounded"
	bordersSquare  = "square"
	bordersNone    = "none"

	densityCompact = "compact"
	densityNormal  = "normal"
)

const workingVerb = "En cours…"

type colorValue string

func (v colorValue) glamour() string {
	if index, ok := ansiIndex(string(v)); ok {
		return strconv.Itoa(index)
	}
	return string(v)
}

func (v colorValue) color() lipgloss.TerminalColor {
	if index, ok := ansiIndex(string(v)); ok {
		return lipgloss.ANSIColor(index)
	}
	return lipgloss.Color(string(v))
}

func ansiIndex(v string) (int, bool) {
	rest, ok := strings.CutPrefix(v, "ansi:")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 0 || n > 15 {
		return 0, false
	}
	return n, true
}

func validColor(v string) bool {
	if _, ok := ansiIndex(v); ok {
		return true
	}
	return len(v) == 7 && v[0] == '#' && isHex(v[1:])
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

type palette struct {
	Text    colorValue
	Dim     colorValue
	Accent  colorValue
	Add     colorValue
	Del     colorValue
	Wait    colorValue
	Err     colorValue
	Sel     colorValue
	SelText colorValue
}

type renderOptions struct {
	Glyphs  string
	Borders string
	Density string
	Bold    bool
	Italic  bool
}

type Theme struct {
	ID     string
	Label  string
	Colors palette
	Render renderOptions
}

func builtinThemes() []Theme {
	defaultRender := renderOptions{Glyphs: glyphsUnicode, Borders: bordersRounded, Density: densityNormal, Bold: true, Italic: true}
	return []Theme{
		{
			ID: "sombre", Label: "Sombre", Render: defaultRender,
			Colors: palette{Text: "#e4e4e4", Dim: "#8f8f8f", Accent: "#e4e4e4", Add: "#7fb08a", Del: "#d0706b", Wait: "#c9a55a", Err: "#d0706b", Sel: "#e4e4e4", SelText: "#111111"},
		},
		{
			ID: "clair", Label: "Clair", Render: defaultRender,
			Colors: palette{Text: "#1a1a1a", Dim: "#666666", Accent: "#1a1a1a", Add: "#2f7a45", Del: "#b03a35", Wait: "#8a6a1c", Err: "#b03a35", Sel: "#1a1a1a", SelText: "#ffffff"},
		},
		{
			ID: "terminal", Label: "Terminal", Render: defaultRender,
			Colors: palette{Text: "ansi:7", Dim: "ansi:8", Accent: "ansi:6", Add: "ansi:2", Del: "ansi:1", Wait: "ansi:3", Err: "ansi:1", Sel: "ansi:7", SelText: "ansi:0"},
		},
		{
			ID: "contraste", Label: "Contraste élevé", Render: defaultRender,
			Colors: palette{Text: "#ffffff", Dim: "#d0d0d0", Accent: "#ffff00", Add: "#5cff8a", Del: "#ff6b6b", Wait: "#ffd700", Err: "#ff6b6b", Sel: "#ffffff", SelText: "#000000"},
		},
	}
}

func builtinTheme(id string) (Theme, bool) {
	for _, th := range builtinThemes() {
		if th.ID == id {
			return th, true
		}
	}
	return Theme{}, false
}

type glyphSet struct {
	tool, result, cursor, unread, thinking, notice, failure, success string
	fold, moreLines, bolt, prompt, ellipsis, divider, ruleChar       string
	gaugeFull, gaugeEmpty, brand, agent, active                      string
	families                                                         map[string]string
	spin                                                             []string
}

func (g glyphSet) familyGlyph(family string) string {
	if mark, ok := g.families[family]; ok {
		return mark
	}
	return g.tool
}

func (g glyphSet) agentTitle() string {
	return g.agent + " Herrscher"
}

func unicodeGlyphs() glyphSet {
	return glyphSet{
		tool: "●", result: "⎿", cursor: "▸", unread: "●", thinking: "○", notice: "·",
		failure: "✗", success: "✓", fold: "▸", moreLines: "↵", bolt: "●", prompt: ">",
		ellipsis: "…", divider: "│", ruleChar: "─", gaugeFull: "█", gaugeEmpty: "░",
		brand: "──◂ HERRSCHER ▸──", agent: "⚡", active: "◆",
		families: map[string]string{
			familyRead: "◇", familyWrite: "◈", familyRun: "▸", familySearch: "⌕", familyWeb: "⌖", familyAgent: "◆",
		},
		spin: []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"},
	}
}

func nerdGlyphs() glyphSet {
	g := unicodeGlyphs()
	g.tool, g.unread, g.bolt = "\uf111", "\uf111", "●"
	g.thinking, g.cursor, g.fold = "\uf10c", "\uf0da", "\uf0da"
	g.failure, g.success = "\uf00d", "\uf00c"
	return g
}

func asciiGlyphs() glyphSet {
	return glyphSet{
		tool: "*", result: "\\", cursor: ">", unread: "*", thinking: "o", notice: ".",
		failure: "x", success: "v", fold: ">", moreLines: "<", bolt: "*", prompt: ">",
		ellipsis: "...", divider: "|", ruleChar: "-", gaugeFull: "#", gaugeEmpty: ".",
		brand: "--< HERRSCHER >--", agent: "*", active: "*",
		families: map[string]string{
			familyRead: "=", familyWrite: "+", familyRun: ">", familySearch: "?", familyWeb: "@", familyAgent: "*",
		},
		spin: []string{"|", "/", "-", "\\"},
	}
}

func glyphsFor(name string) glyphSet {
	switch name {
	case glyphsNerd:
		return nerdGlyphs()
	case glyphsASCII:
		return asciiGlyphs()
	default:
		return unicodeGlyphs()
	}
}

type boxSet struct {
	tl, tr, bl, br, h, v string
}

func boxesFor(borders string) boxSet {
	switch borders {
	case bordersSquare:
		return boxSet{"┌", "┐", "└", "┘", "─", "│"}
	case bordersNone:
		return boxSet{" ", " ", " ", " ", " ", " "}
	default:
		return boxSet{"╭", "╮", "╰", "╯", "─", "│"}
	}
}

func asciiBoxes() boxSet {
	return boxSet{"+", "+", "+", "+", "-", "|"}
}

type uiStyles struct {
	text       lipgloss.Style
	dim        lipgloss.Style
	accent     lipgloss.Style
	user       lipgloss.Style
	green      lipgloss.Style
	red        lipgloss.Style
	warn       lipgloss.Style
	del        lipgloss.Style
	spinner    lipgloss.Style
	tool       lipgloss.Style
	thinking   lipgloss.Style
	scrollback lipgloss.Style
	link       lipgloss.Style
	chip       lipgloss.Style
	overlaySel lipgloss.Style
	sep        string
	heading    string

	glyphs  glyphSet
	borders string
	ascii   bool
	compact bool
	bold    bool
}

func newStyles(th Theme, noColor bool) uiStyles {
	pal, look := th.Colors, th.Render
	paint := func(v colorValue) lipgloss.Style {
		if noColor {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Foreground(v.color())
	}
	dim := paint(pal.Dim)
	if noColor {
		dim = dim.Faint(true)
	}
	selected := lipgloss.NewStyle().Background(pal.Sel.color()).Foreground(pal.SelText.color())
	if noColor {
		selected = lipgloss.NewStyle().Reverse(true)
	}
	selected = selected.Bold(look.Bold)
	glyphs := glyphsFor(look.Glyphs)
	return uiStyles{
		text:       paint(pal.Text),
		dim:        dim,
		accent:     paint(pal.Accent),
		user:       dim,
		green:      paint(pal.Add),
		red:        paint(pal.Err),
		warn:       paint(pal.Wait),
		del:        paint(pal.Del),
		spinner:    paint(pal.Accent),
		tool:       paint(pal.Accent),
		thinking:   paint(pal.Dim).Italic(look.Italic),
		scrollback: dim.Faint(true),
		link:       paint(pal.Accent).Underline(true),
		chip:       selected.Padding(0, 1),
		overlaySel: selected,
		sep:        dim.Render(" " + glyphs.divider + " "),
		heading:    headingColor(pal.Accent, noColor),
		glyphs:     glyphs,
		borders:    look.Borders,
		ascii:      look.Glyphs == glyphsASCII,
		compact:    look.Density == densityCompact,
		bold:       look.Bold,
	}
}

func headingColor(accent colorValue, noColor bool) string {
	if noColor {
		return ""
	}
	return accent.glamour()
}

func (s uiStyles) finish(view string) string {
	if !s.ascii {
		return view
	}
	return asciiOnly(view)
}

func (s uiStyles) boxes(supported bool) boxSet {
	if !supported || s.ascii {
		return asciiBoxes()
	}
	return boxesFor(s.borders)
}

func (s uiStyles) ruleGlyph() string {
	if s.borders == bordersNone {
		return " "
	}
	return s.glyphs.ruleChar
}

func (s uiStyles) rule(style lipgloss.Style, width int) string {
	if width < 1 {
		return ""
	}
	return style.Render(strings.Repeat(s.ruleGlyph(), width))
}

func (s uiStyles) titledRule(style lipgloss.Style, title string, width int) string {
	if width < 1 {
		return ""
	}
	lead := s.ruleGlyph() + titleGap
	tail := titleGap
	spent := lipgloss.Width(lead) + lipgloss.Width(title) + lipgloss.Width(tail)
	if spent >= width {
		return s.rule(style, width)
	}
	return style.Render(lead) + style.Bold(s.bold).Render(title) +
		style.Render(tail+strings.Repeat(s.ruleGlyph(), width-spent))
}

func (s uiStyles) spinFrame(index int) string {
	return s.glyphs.spin[index%len(s.glyphs.spin)]
}
