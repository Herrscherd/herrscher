package manager

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

var catalogKeyShape = regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)

func TestSentinelsReadAsEnglishSentences(t *testing.T) {
	sentinels := []error{
		ErrCloneUnsupported, ErrConflictingChannel, ErrConflictingPlacement,
		ErrExplicitCmdRefused, ErrMissingChannel, ErrMissingName, ErrMissingPath,
		ErrMissingSoul, ErrUnsupportedLang, ErrMissingTo, ErrModelRequired,
		ErrNoHome, ErrNoHostRegistered, ErrServiceControlUnavailable, ErrSessionLimit,
		ErrAccountVendor, ErrAccountHost, ErrAccountRemote,
	}
	for _, sentinel := range sentinels {
		text := sentinel.Error()
		if catalogKeyShape.MatchString(text) || !strings.Contains(text, " ") {
			t.Errorf("sentinel reads as a catalog key, not a sentence: %q", text)
		}
	}
}

func TestAccountVendorErrorIsFrozenInEnglishAndFrench(t *testing.T) {
	unresolved := func(string, string, string) (string, error) { return "", errors.New("no backend") }
	english := newAccountHandler(t, func(_, _ string) error { return nil }, unresolved)
	err := english.checkAccount(runtimeChoice{}, "ghost")
	if want := `account "ghost": no backend vendor is known for this session: pass --vendor, or set HERRSCHER_BACKEND: no backend`; err == nil || err.Error() != want {
		t.Fatalf("Error() = %v, want %q", err, want)
	}
	if !errors.Is(err, ErrAccountVendor) {
		t.Fatal("the account vendor error must still match ErrAccountVendor")
	}
	if want := `unexpected error: account "ghost": no backend vendor is known for this session: pass --vendor, or set HERRSCHER_BACKEND: no backend`; renderNotice(t, i18n.English, err) != want {
		t.Fatalf("en notice = %q, want %q", renderNotice(t, i18n.English, err), want)
	}
	french := newAccountHandler(t, func(_, _ string) error { return nil }, unresolved)
	french.SetLanguage(i18n.French)
	frErr := french.checkAccount(runtimeChoice{}, "ghost")
	if want := `erreur inattendue : compte "ghost" : aucun fournisseur de backend n'est connu pour cette session : passez --vendor, ou réglez HERRSCHER_BACKEND: no backend`; renderNotice(t, i18n.French, frErr) != want {
		t.Fatalf("fr notice = %q, want %q", renderNotice(t, i18n.French, frErr), want)
	}
}
