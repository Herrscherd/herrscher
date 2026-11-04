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
)

const (
	obelFamily         = "Obel Mono"
	obelMaskedFamily   = "Obel Mono Masked"
	obelInstallCommand = "curl -fsSL https://typographic.systems/install.sh | sh"
	obelInstallNote    = "sans sudo, vérifie le SHA-256 de chaque zip · licence SIL OFL 1.1"
	obelMaskFeature    = "ss01"
	obelMaskNote       = "ss01 masque les clés d’API à l’écran, le presse-papiers garde tout"
	obelMaskedNote     = "sans réglage de features : famille « Obel Mono Masked »"
	obelFontKeys       = "c copier la commande · y copier la config · f fermer"
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

func fontStatus(presence fontPresence) string {
	switch presence {
	case fontInstalled:
		return "installée"
	case fontAbsent:
		return "absente"
	case fontUnknown:
		return "inconnue"
	case fontUnchecked:
	}
	return "vérification…"
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
	name      string
	file      string
	placement string
	config    []string
	masked    bool
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
			placement: "à placer dans config = wezterm.config_builder()",
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
		m.flash = "copie impossible"
		return
	}
	if err := m.clip.WriteText(m.runContext(), text); err != nil {
		m.flash = "copie impossible : " + err.Error()
		return
	}
	m.flash = label + " copiée"
}

func (m *model) obelInstallLines(width, rows int) []string {
	styles := m.st
	setup := terminalSetupFor(m.terminal)
	terminalHead := themeSectionHead(styles, "terminal détecté", setup.name+" · "+setup.file, width)
	if m.terminal == terminalUnidentified {
		terminalHead = themeSectionHead(styles, "terminal non reconnu", "", width)
	}
	lines := []string{
		themeSectionHead(styles, "installer Obel Mono", "", width),
		"   " + styles.accent.Render("$") + " " + styles.text.Render(obelInstallCommand),
		"   " + styles.dim.Render(obelInstallNote),
		"",
		"   " + styles.dim.Render(obelFontKeys),
		"",
		terminalHead,
	}
	if setup.placement != "" {
		lines = append(lines, "   "+styles.dim.Render(setup.placement))
	}
	for _, line := range setup.config {
		lines = append(lines, "   "+styles.text.Render(line))
	}
	lines = append(lines, "   "+styles.dim.Render(obelMaskNote))
	if setup.masked {
		lines = append(lines, "   "+styles.dim.Render(obelMaskedNote))
	}
	for index := range lines {
		lines[index] = cutText(lines[index], width)
	}
	return clipLines(lines, rows)
}

func (m *model) obelConfigText() string {
	return strings.Join(terminalSetupFor(m.terminal).config, "\n")
}

func themeFontRow(styles uiStyles, presence fontPresence) string {
	status := styles.dim.Render(fontStatus(presence))
	if presence == fontInstalled {
		status = styles.accent.Render("✓ " + fontStatus(presence))
	}
	return "   " + padTo("police", themeKeyColumn) + styles.text.Render(obelFamily+" · recommandée") + "  " + status + "  " + styles.dim.Render("f")
}
