package clitext

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func stateWithConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HERRSCHER_STATE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestClientLanguageTakesTheConfigLanguageAheadOfTheLocale(t *testing.T) {
	stateWithConfig(t, `{"language": "fr"}`)
	t.Setenv("HERRSCHER_LANG", "")
	t.Setenv("LANG", "en_US.UTF-8")

	if got := ClientLanguage(); got != i18n.French {
		t.Fatalf("ClientLanguage with language fr and LANG=en = %s, want fr", got)
	}
}

func TestClientLanguageHerrscherLangBeatsTheConfigLanguage(t *testing.T) {
	stateWithConfig(t, `{"language": "fr"}`)
	t.Setenv("HERRSCHER_LANG", "en")
	t.Setenv("LANG", "fr_FR.UTF-8")

	if got := ClientLanguage(); got != i18n.English {
		t.Fatalf("ClientLanguage with HERRSCHER_LANG=en and language fr = %s, want en", got)
	}
}
