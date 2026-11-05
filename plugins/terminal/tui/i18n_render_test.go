package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestFrozenRendersInEachLanguage(t *testing.T) {
	cases := []struct {
		name   string
		lang   i18n.Language
		render func(*model) string
		want   string
	}{
		{"palette help en", i18n.English, paletteHelp, "show the shortcuts"},
		{"palette help fr", i18n.French, paletteHelp, "afficher les raccourcis"},
		{"footer disconnected en", i18n.English, disconnectedFooter, "· disconnected"},
		{"footer disconnected fr", i18n.French, disconnectedFooter, "· déconnecté"},
		{"copy nothing en", i18n.English, copyNothingFlash, "nothing to copy here"},
		{"copy nothing fr", i18n.French, copyNothingFlash, "rien à copier ici"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newModel(nil)
			m.localize(tc.lang)
			if got := tc.render(m); got != tc.want {
				t.Fatalf("render = %q, want %q", got, tc.want)
			}
		})
	}
}

func paletteHelp(m *model) string {
	return localCommands(m.tr)[0].Desc
}

func disconnectedFooter(m *model) string {
	m.tabs["main"] = &tab{channel: "main", label: "main", disconnected: true}
	m.active = "main"
	return ansi.Strip(m.footer(80))
}

func copyNothingFlash(m *model) string {
	m.tabs["main"] = &tab{channel: "main", label: "main"}
	m.active = "main"
	m.clip = fakeClipboard{}
	m.copyTarget(copyReply)
	return m.flash
}
