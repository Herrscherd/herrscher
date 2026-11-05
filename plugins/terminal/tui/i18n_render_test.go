package tui

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
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

func TestSharedCliTextFollowsTheTuiLanguage(t *testing.T) {
	trans := newTranslator()
	if got := trans.in(i18n.French).shared("cli.manage.state.added"); got != "ajouté" {
		t.Fatalf("the shared CLI text must render in French, got %q", got)
	}
	if got := trans.in(i18n.English).shared("cli.manage.state.added"); got != "added" {
		t.Fatalf("the shared CLI text must render in English, got %q", got)
	}
}

func TestSharedCliTextLoadsTheCatalogOnce(t *testing.T) {
	trans := newTranslator().in(i18n.French)
	allocs := testing.AllocsPerRun(3, func() {
		for range 200 {
			trans.shared("cli.manage.pinned_tag")
		}
	})
	const budget = 150000
	if allocs > budget {
		t.Fatalf("200 shared texts allocated %.0f times, above the budget of %d allocations: the CLI catalogue is reloaded per text", allocs, budget)
	}
}

func TestLocalizedReasonTranslatesWrappedSentinels(t *testing.T) {
	cases := []struct {
		name     string
		lang     i18n.Language
		sentinel error
		want     string
	}{
		{"unknown key en", i18n.English, errUnknownKey, "unknown key : x"},
		{"unknown key fr", i18n.French, errUnknownKey, "clé inconnue : x"},
		{"duplicate binding en", i18n.English, errDuplicateBinding, "action bound twice in the same table : x"},
		{"duplicate binding fr", i18n.French, errDuplicateBinding, "action liée deux fois dans une même table : x"},
		{"not a repo en", i18n.English, errNotRepo, "no git repository in this session's folder : x"},
		{"not a repo fr", i18n.French, errNotRepo, "aucun dépôt git dans le dossier de cette session : x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("%w : x", tc.sentinel)
			got := localizedReason(newTranslator().in(tc.lang), wrapped)
			if got != tc.want {
				t.Fatalf("localizedReason(%q) = %q, want %q", wrapped, got, tc.want)
			}
		})
	}
}

func TestLocalizedReasonKeepsUnknownErrorsRaw(t *testing.T) {
	raw := errors.New("open /etc/x: permission denied")
	if got := localizedReason(newTranslator().in(i18n.French), raw); got != raw.Error() {
		t.Fatalf("an error outside the table must keep its text, got %q", got)
	}
}

func TestSharedCliTextLogsFailureOnce(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	trans := newTranslator().in(i18n.French)
	for range 5 {
		if got := trans.shared(clitext.Key("cli.missing.key")); got != "cli.missing.key" {
			t.Fatalf("a failed shared text must return its key, got %q", got)
		}
	}
	if n := strings.Count(logs.String(), "tui shared message not rendered"); n != 1 {
		t.Fatalf("five failing shared texts logged %d times, want once", n)
	}
}
