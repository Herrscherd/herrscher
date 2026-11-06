package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	obelFamily         = "Obel Mono"
	obelMaskedFamily   = "Obel Mono Masked"
	obelInstallCommand = "curl -fsSL https://typographic.systems/install.sh | sh"
	obelMaskFeature    = "ss01"
	obelUserFontGlob   = "ObelMono*.ttf"
	fontProbeLimit     = 2 * time.Second
	fontListSeparator  = ","
)

type fontPresence int

const (
	fontUnchecked fontPresence = iota
	fontInstalled
	fontAbsent
	fontUnknown
)

type fontProbeMsg struct {
	presence fontPresence
}

func fontProbeCmd() tea.Cmd {
	return func() tea.Msg {
		return fontProbeMsg{presence: probeObelFont()}
	}
}

func probeObelFont() fontPresence {
	switch runtime.GOOS {
	case "linux":
		return probeFontconfig()
	case "darwin":
		return probeUserFonts()
	}
	return fontUnknown
}

func probeFontconfig() fontPresence {
	ctx, cancel := context.WithTimeout(context.Background(), fontProbeLimit)
	defer cancel()
	listing, err := exec.CommandContext(ctx, "fc-list", ":", "family").Output()
	if err != nil {
		return fontUnknown
	}
	return familyPresence(string(listing))
}

func familyPresence(listing string) fontPresence {
	for _, line := range strings.Split(listing, "\n") {
		for _, family := range strings.Split(line, fontListSeparator) {
			switch strings.TrimSpace(family) {
			case obelFamily, obelMaskedFamily:
				return fontInstalled
			}
		}
	}
	return fontAbsent
}

func probeUserFonts() fontPresence {
	home, err := os.UserHomeDir()
	if err != nil {
		return fontUnknown
	}
	matches, err := filepath.Glob(filepath.Join(home, "Library", "Fonts", obelUserFontGlob))
	if err != nil {
		return fontUnknown
	}
	if len(matches) == 0 {
		return fontAbsent
	}
	return fontInstalled
}

func fontStatus(trans translator, presence fontPresence) string {
	switch presence {
	case fontInstalled:
		return trans.t("tui.theme.font.installed", nil)
	case fontAbsent:
		return trans.t("tui.theme.font.absent", nil)
	case fontUnknown:
		return trans.t("tui.theme.font.unknown", nil)
	case fontUnchecked:
	}
	return trans.t("tui.theme.font.checking", nil)
}

type terminalKind int

const (
	terminalUnidentified terminalKind = iota
	terminalGhostty
	terminalKitty
	terminalAlacritty
	terminalFoot
	terminalWezTerm
)

func detectTerminal(lookup func(string) (string, bool)) terminalKind {
	program, _ := lookup("TERM_PROGRAM")
	term, _ := lookup("TERM")
	present := func(name string) bool {
		_, ok := lookup(name)
		return ok
	}
	switch {
	case present("GHOSTTY_RESOURCES_DIR") || program == "ghostty":
		return terminalGhostty
	case present("WEZTERM_EXECUTABLE") || program == "WezTerm":
		return terminalWezTerm
	case present("KITTY_WINDOW_ID"):
		return terminalKitty
	case present("ALACRITTY_SOCKET") || program == "alacritty":
		return terminalAlacritty
	case strings.HasPrefix(term, "foot"):
		return terminalFoot
	}
	return terminalUnidentified
}

type terminalSetup struct {
	name   string
	file   string
	before []string
	config []string
	after  []string
	masked bool
}

func terminalSetupFor(kind terminalKind) terminalSetup {
	switch kind {
	case terminalGhostty:
		return terminalSetup{
			name: "Ghostty", file: "~/.config/ghostty/config",
			config: []string{`font-family = "Obel Mono"`, "font-feature = ss01"},
		}
	case terminalKitty:
		return terminalSetup{
			name: "kitty", file: "~/.config/kitty/kitty.conf",
			config: []string{"font_family " + obelMaskedFamily}, masked: true,
		}
	case terminalAlacritty:
		return terminalSetup{
			name: "Alacritty", file: "~/.config/alacritty/alacritty.toml",
			config: []string{"[font.normal]", `family = "` + obelMaskedFamily + `"`}, masked: true,
		}
	case terminalFoot:
		return terminalSetup{
			name: "foot", file: "~/.config/foot/foot.ini",
			config: []string{"[main]", "font=" + obelFamily + ":fontfeatures=" + obelMaskFeature},
		}
	case terminalWezTerm:
		return terminalSetup{
			name: "WezTerm", file: "~/.wezterm.lua",
			before: []string{"local wezterm = require 'wezterm'", "local config = wezterm.config_builder()"},
			after:  []string{"return config"},
			config: []string{
				"config.font = wezterm.font '" + obelFamily + "'",
				"config.harfbuzz_features = { '" + obelMaskFeature + "' }",
			},
		}
	case terminalUnidentified:
	}
	return terminalSetup{config: []string{obelFamily}, masked: true}
}

func (m *model) copyObel(text, label string) {
	if m.clip == nil {
		m.flash = m.tr.t("tui.theme.copy_failed", nil)
		return
	}
	if err := m.clip.WriteText(m.runContext(), text); err != nil {
		m.flash = m.tr.t("tui.copy.failed", map[string]any{tplArgReason: localizedReason(m.tr, err)})
		return
	}
	m.flash = m.tr.t("tui.theme.copied", map[string]any{tplArgLabel: label})
}

func (m *model) obelInstallLines(width, rows int) []string {
	styles := m.st
	setup := terminalSetupFor(m.terminal)
	terminalHead := themeSectionHead(styles, m.tr.t("tui.theme.font.head_terminal", nil), setup.name+" · "+setup.file, width)
	if m.terminal == terminalUnidentified {
		terminalHead = themeSectionHead(styles, m.tr.t("tui.theme.font.head_unknown", nil), "", width)
	}
	lines := []string{
		themeSectionHead(styles, m.tr.t("tui.theme.font.head_install", nil), "", width),
		"   " + styles.accent.Render("$") + " " + styles.text.Render(obelInstallCommand),
	}
	lines = append(lines, noteLines(styles.dim, m.tr.t("tui.theme.font.install_note", nil), width)...)
	lines = append(lines, "")
	lines = append(lines, noteLines(styles.dim, m.tr.t("tui.theme.font.keys", nil), width)...)
	lines = append(lines, "", terminalHead)
	for _, line := range setup.before {
		lines = append(lines, "   "+styles.dim.Render(line))
	}
	for _, line := range setup.config {
		lines = append(lines, "   "+styles.text.Render(line))
	}
	for _, line := range setup.after {
		lines = append(lines, "   "+styles.dim.Render(line))
	}
	lines = append(lines, noteLines(styles.dim, m.tr.t("tui.theme.font.mask_note", nil), width)...)
	if setup.masked {
		lines = append(lines, noteLines(styles.dim, m.tr.t("tui.theme.font.masked_note", nil), width)...)
	}
	for index := range lines {
		lines[index] = cutText(lines[index], width)
	}
	return clipLines(lines, rows)
}

const noteIndent = "   "

func noteLines(style lipgloss.Style, note string, width int) []string {
	wrapped := strings.Split(ansi.Wrap(note, max(width-len(noteIndent), 1), " "), "\n")
	lines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, noteIndent+style.Render(line))
	}
	return lines
}

func (m *model) obelConfigText() string {
	return strings.Join(terminalSetupFor(m.terminal).config, "\n")
}

func themeFontRow(styles uiStyles, trans translator, presence fontPresence) string {
	status := styles.dim.Render(fontStatus(trans, presence))
	if presence == fontInstalled {
		status = styles.accent.Render("✓ " + fontStatus(trans, presence))
	}
	family := trans.t("tui.theme.font.family_line", map[string]any{"Family": obelFamily})
	return "   " + padTo(trans.t("tui.theme.font.row", nil), themeKeyColumn) + styles.text.Render(family) + "  " + status + "  " + styles.dim.Render("f")
}
