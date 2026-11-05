package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const workingVerb = "En cours…"

const borderNoneName = "none"

const (
	themeSombre    = "sombre"
	themeClair     = "clair"
	themeTerminal  = "terminal"
	themeContraste = "contraste"
)

type glyphMode int

const (
	glyphsUnicode glyphMode = iota + 1
	glyphsNerd
	glyphsASCII
)

func glyphModes() []glyphMode {
	return []glyphMode{glyphsUnicode, glyphsNerd, glyphsASCII}
}

func (mode glyphMode) String() string {
	switch mode {
	case glyphsUnicode:
		return "unicode"
	case glyphsNerd:
		return "nerd"
	case glyphsASCII:
		return "ascii"
	}
	return ""
}

type borderStyle int

const (
	borderRounded borderStyle = iota + 1
	borderSquare
	borderNone
)

func borderStyles() []borderStyle {
	return []borderStyle{borderRounded, borderSquare, borderNone}
}

func (style borderStyle) String() string {
	switch style {
	case borderRounded:
		return "rounded"
	case borderSquare:
		return "square"
	case borderNone:
		return borderNoneName
	}
	return ""
}

type density int

const (
	densityNormal density = iota + 1
	densityCompact
)

func densities() []density {
	return []density{densityNormal, densityCompact}
}

func (level density) String() string {
	switch level {
	case densityNormal:
		return "normal"
	case densityCompact:
		return "compact"
	}
	return ""
}

type closedOption interface {
	comparable
	String() string
}

func parseOption[Choice closedOption](key, value string, options []Choice) (Choice, error) {
	for _, option := range options {
		if option.String() == value {
			return option, nil
		}
	}
	var zero Choice
	return zero, fmt.Errorf("%w pour %s : %q (attendu %s)", errUnknownOption, key, value, joinOptions(options))
}

func joinOptions[Choice closedOption](options []Choice) string {
	names := make([]string, 0, len(options))
	for _, option := range options {
		names = append(names, option.String())
	}
	return strings.Join(names, ", ")
}

func nextOption[Choice comparable](options []Choice, current Choice) Choice {
	for index, option := range options {
		if option == current {
			return options[(index+1)%len(options)]
		}
	}
	return options[0]
}

type colorValue string

func (value colorValue) glamour() string {
	if index, ok := ansiIndex(string(value)); ok {
		return strconv.Itoa(index)
	}
	return string(value)
}

func (value colorValue) color() lipgloss.TerminalColor {
	if index, ok := ansiIndex(string(value)); ok {
		return lipgloss.ANSIColor(index)
	}
	return lipgloss.Color(string(value))
}

const (
	ansiPrefix   = "ansi:"
	ansiMaxIndex = 15
	hexColorLen  = 7
	hexDigits    = "0123456789abcdefABCDEF"
)

func ansiIndex(value string) (int, bool) {
	rest, ok := strings.CutPrefix(value, ansiPrefix)
	if !ok {
		return 0, false
	}
	index, err := strconv.Atoi(rest)
	if err != nil || index < 0 || index > ansiMaxIndex {
		return 0, false
	}
	return index, true
}

func validColor(value string) bool {
	if _, ok := ansiIndex(value); ok {
		return true
	}
	return len(value) == hexColorLen && value[0] == '#' && isHex(value[1:])
}

func isHex(text string) bool {
	for _, character := range text {
		if !strings.ContainsRune(hexDigits, character) {
			return false
		}
	}
	return true
}

type palette struct {
	Text    colorValue
	Dim     colorValue
	Accent  colorValue
	Violet  colorValue
	Add     colorValue
	AddBg   colorValue
	Del     colorValue
	DelBg   colorValue
	Wait    colorValue
	Err     colorValue
	Sel     colorValue
	SelText colorValue
}

type renderOptions struct {
	Glyphs  glyphMode
	Borders borderStyle
	Density density
	Bold    bool
	Italic  bool
}

func defaultRenderOptions() renderOptions {
	return renderOptions{Glyphs: glyphsUnicode, Borders: borderRounded, Density: densityNormal, Bold: true, Italic: true}
}

// Theme est un thème complet : couleurs de la palette et réglages de rendu.
type Theme struct {
	ID     string
	Label  string
	Colors palette
	Render renderOptions
}

const (
	sombreInk    = "#e4e4e4"
	sombreDim    = "#8a8a88"
	sombreAccent = "#ff3c05"
	clairInk     = "#1a1a1a"
	clairDim     = "#666666"
	clairAccent  = "#c42d00"
	contrastInk  = "#ffffff"
)

func builtinThemes() []Theme {
	render := defaultRenderOptions()
	return []Theme{
		{
			ID: themeSombre, Label: "Sombre", Render: render,
			Colors: palette{Text: sombreInk, Dim: sombreDim, Accent: sombreAccent, Violet: sombreInk, Add: "#7fb08a", AddBg: "#14231a", Del: "#d0706b", DelBg: "#2a1515", Wait: "#c9a55a", Err: "#d0706b", Sel: sombreInk, SelText: "#111111"},
		},
		{
			ID: themeClair, Label: "Clair", Render: render,
			Colors: palette{Text: clairInk, Dim: clairDim, Accent: clairAccent, Violet: clairInk, Add: "#2f7a45", AddBg: "#e3f1e6", Del: "#b03a35", DelBg: "#f7e1e0", Wait: "#8a6a1c", Err: "#b03a35", Sel: clairInk, SelText: "#ffffff"},
		},
		{
			ID: themeTerminal, Label: "Terminal", Render: render,
			Colors: palette{Text: "ansi:7", Dim: "ansi:8", Accent: "ansi:6", Violet: "ansi:5", Add: "ansi:2", AddBg: "#0c2a1d", Del: "ansi:1", DelBg: "#2c1111", Wait: "ansi:3", Err: "ansi:1", Sel: "ansi:7", SelText: "ansi:0"},
		},
		{
			ID: themeContraste, Label: "Contraste élevé", Render: render,
			Colors: palette{Text: contrastInk, Dim: "#d0d0d0", Accent: "#ffff00", Violet: contrastInk, Add: "#5cff8a", AddBg: "#003314", Del: "#ff6b6b", DelBg: "#4a0000", Wait: "#ffd700", Err: "#ff6b6b", Sel: contrastInk, SelText: "#000000"},
		},
	}
}

func builtinTheme(themeName string) (Theme, bool) {
	themes := builtinThemes()
	for index := range themes {
		if themes[index].ID == themeName {
			return themes[index], true
		}
	}
	return Theme{}, false
}

type glyphSet struct {
	tool, result, cursor, unread, thinking, notice, failure, success string
	fold, moreLines, bolt, prompt, ellipsis, divider, ruleChar       string
	gaugeFull, gaugeEmpty, brand, agent, active                      string
	selected, attention, open, railMark, filet, minus                string
	expanded, branchMid, branchEnd, omitted, userBar                 string
	ticked, unticked                                                 string
	caret, reasonArrow, warn, note                                   string
	waiting, review                                                  string
	fileGo, fileDoc, branch, shellPrompt                             string
	families                                                         map[string]string
	spin                                                             []string
}

func (g glyphSet) familyGlyph(family string) string {
	if mark, ok := g.families[family]; ok {
		return mark
	}
	return g.tool
}

func unicodeGlyphs() glyphSet {
	return glyphSet{
		tool: "●", result: "└", cursor: "▸", unread: "●", thinking: "○", notice: "·",
		failure: "✗", success: "✓", fold: "▸", moreLines: "↵", bolt: "●", prompt: ">", shellPrompt: "$",
		ellipsis: "…", divider: "│", ruleChar: "─", gaugeFull: gaugeFullGlyph, gaugeEmpty: gaugeEmptyGlyph,
		brand: "──◂ HERRSCHER ▸──", agent: "⚡", active: "◆",
		selected: "▸", attention: "!", open: "○", railMark: "━", filet: "│", minus: "−",
		expanded: "▾", branchMid: "├", branchEnd: "└", omitted: "⋯", userBar: "┃",
		ticked: "[✓]", unticked: "[ ]",
		caret: "▌", reasonArrow: "›", warn: "⚠", note: "↪", waiting: "◔", review: "◎",
		families: map[string]string{
			familyRead: "◇", familyWrite: "◈", familyRun: "▸", familySearch: "⌕", familyWeb: "⌖", familyAgent: "◆",
		},
		spin: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
	}
}

func nerdGlyphs() glyphSet {
	glyphs := unicodeGlyphs()
	glyphs.tool, glyphs.unread, glyphs.bolt = "", "", "●"
	glyphs.thinking, glyphs.cursor, glyphs.fold = "", "", ""
	glyphs.failure, glyphs.success = "", ""
	glyphs.fileGo, glyphs.fileDoc, glyphs.branch = "", "", ""
	return glyphs
}

func asciiGlyphs() glyphSet {
	return glyphSet{
		tool: "*", result: "\\", cursor: ">", unread: "*", thinking: "o", notice: ".",
		failure: "x", success: "v", fold: ">", moreLines: "<", bolt: "*", prompt: ">", shellPrompt: "$",
		ellipsis: asciiEllipsis, divider: "|", ruleChar: "-", gaugeFull: "#", gaugeEmpty: ".",
		brand: "--< HERRSCHER >--", agent: "*", active: "*",
		selected: ">", attention: "!", open: "o", railMark: "=", filet: "|", minus: "-",
		expanded: "v", branchMid: "+", branchEnd: "\\", omitted: asciiEllipsis, userBar: "|",
		ticked: "[v]", unticked: "[ ]",
		caret: "_", reasonArrow: ">", warn: "!", note: "+", waiting: "~", review: "?",
		families: map[string]string{
			familyRead: "=", familyWrite: "+", familyRun: ">", familySearch: "?", familyWeb: "@", familyAgent: "*",
		},
		spin: []string{"|", "/", "-", "\\"},
	}
}

const (
	gaugeFullGlyph    = "━"
	gaugeEmptyGlyph   = "─"
	noColorGaugeEmpty = "░"
)

func gaugeGlyphsFor(glyphs glyphSet, noColor bool) glyphSet {
	if noColor && glyphs.gaugeEmpty == gaugeEmptyGlyph {
		glyphs.gaugeEmpty = noColorGaugeEmpty
	}
	return glyphs
}

func glyphsFor(mode glyphMode) glyphSet {
	switch mode {
	case glyphsNerd:
		return nerdGlyphs()
	case glyphsASCII:
		return asciiGlyphs()
	case glyphsUnicode:
		return unicodeGlyphs()
	}
	return unicodeGlyphs()
}

type boxSet struct {
	tl, tr, bl, br, h, v string
}

func boxesFor(style borderStyle) boxSet {
	switch style {
	case borderSquare:
		return boxSet{"┌", "┐", "└", "┘", "─", "│"}
	case borderNone:
		return boxSet{" ", " ", " ", " ", " ", " "}
	case borderRounded:
		return boxSet{"╭", "╮", "╰", "╯", "─", "│"}
	}
	return boxSet{"╭", "╮", "╰", "╯", "─", "│"}
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
	strong     lipgloss.Style
	violet     lipgloss.Style
	addRow     lipgloss.Style
	delRow     lipgloss.Style
	modeManual lipgloss.Style
	modeEdits  lipgloss.Style
	modePlan   lipgloss.Style
	modeAuto   lipgloss.Style
	sep        string
	heading    string

	glyphs  glyphSet
	borders borderStyle
	ascii   bool
	compact bool
	bold    bool
}

func newStyles(th Theme, noColor bool) uiStyles {
	pal, look := th.Colors, th.Render
	paint := func(value colorValue) lipgloss.Style {
		if noColor {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Foreground(value.color())
	}
	tint := func(foreground, background colorValue) lipgloss.Style {
		if noColor {
			return lipgloss.NewStyle()
		}
		return lipgloss.NewStyle().Foreground(foreground.color()).Background(background.color())
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
	glyphs := gaugeGlyphsFor(glyphsFor(look.Glyphs), noColor)
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
		strong:     paint(pal.Text).Bold(true),
		violet:     paint(pal.Violet),
		addRow:     tint(pal.Add, pal.AddBg),
		delRow:     tint(pal.Del, pal.DelBg),
		modeManual: paint(pal.Accent),
		modeEdits:  paint(pal.Add),
		modePlan:   paint(pal.Violet),
		modeAuto:   paint(pal.Err),
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
	if s.borders == borderNone {
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
