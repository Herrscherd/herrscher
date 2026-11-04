package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestFontListingFindsEitherObelFamily(t *testing.T) {
	cases := map[string]fontPresence{
		"Obel Mono\nDejaVu Sans\n":       fontInstalled,
		"DejaVu Sans,Obel Mono Masked\n": fontInstalled,
		"DejaVu Sans\nObel Monolith\n":   fontAbsent,
		"":                               fontAbsent,
	}
	for listing, want := range cases {
		if got := familyPresence(listing); got != want {
			t.Errorf("listing %q: got %v, want %v", listing, got, want)
		}
	}
}

func TestFontRowShowsEachDetectionState(t *testing.T) {
	cases := map[fontPresence]string{
		fontInstalled: "installée",
		fontAbsent:    "absente",
		fontUnknown:   "inconnue",
		fontUnchecked: "vérification…",
	}
	for presence, want := range cases {
		row := ansi.Strip(themeFontRow(testStyles(), presence))
		if !strings.Contains(row, "police") || !strings.Contains(row, "Obel Mono · recommandée") || !strings.Contains(row, want) {
			t.Errorf("state %v: the font row must show the family and %q, got %q", presence, want, row)
		}
	}
}

func TestTerminalIsDetectedFromItsEnvironment(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want terminalKind
	}{
		{"ghostty", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, terminalGhostty},
		{"kitty", map[string]string{"KITTY_WINDOW_ID": "1"}, terminalKitty},
		{"alacritty", map[string]string{"TERM_PROGRAM": "alacritty"}, terminalAlacritty},
		{"foot", map[string]string{"TERM": "foot-extra"}, terminalFoot},
		{"unknown", map[string]string{"TERM": "xterm-256color"}, terminalUnidentified},
	}
	for _, tc := range cases {
		lookup := func(name string) (string, bool) {
			value, ok := tc.env[name]
			return value, ok
		}
		if got := detectTerminal(lookup); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestInstallBlockFitsEachWindow(t *testing.T) {
	for _, width := range []int{60, 80, 120, 200} {
		m := themeSized(t, width, 20)
		m.handleThemeLetter("f")
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		if len(lines) != 20 {
			t.Fatalf("width %d: the install block must keep the window height, got %d lines", width, len(lines))
		}
		for _, line := range lines {
			if lipgloss.Width(line) > width {
				t.Fatalf("width %d: no line may be wider than the window: %q", width, line)
			}
		}
		if !strings.Contains(ansi.Strip(m.View()), obelInstallCommand) && width >= 80 {
			t.Errorf("width %d: the install command must be shown whole", width)
		}
	}
}

func TestInstallBlockCopiesTheCommandWithC(t *testing.T) {
	m := themeSized(t, 80, 20)
	clip := &fakeWriteClipboard{}
	m.clip = clip
	m.handleThemeLetter("f")
	m.handleThemeLetter("c")
	if clip.wrote != obelInstallCommand {
		t.Fatalf("c must copy the install command, got %q", clip.wrote)
	}
}

func TestInstallBlockCopiesTheConfigWithY(t *testing.T) {
	m := themeSized(t, 80, 20)
	clip := &fakeWriteClipboard{}
	m.clip = clip
	m.terminal = terminalKitty
	m.handleThemeLetter("f")
	m.handleThemeLetter("y")
	if clip.wrote != "font_family "+obelMaskedFamily {
		t.Fatalf("y must copy the terminal configuration, got %q", clip.wrote)
	}
}

func TestEscapeClosesTheInstallBlockBeforeTheTheme(t *testing.T) {
	m := themeSized(t, 80, 20)
	m.handleThemeLetter("f")
	m.handleThemeKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.themeScr == nil || m.themeScr.install {
		t.Fatal("the first escape must close the install block and keep the theme screen open")
	}
	m.handleThemeKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.themeScr != nil {
		t.Fatal("the second escape must leave the theme screen")
	}
}
