package clitext

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

func TestClientLanguageWarnsAndFallsBackWhenConfigIsUnreadable(t *testing.T) {
	stateWithConfig(t, `{"language": `)
	t.Setenv("HERRSCHER_LANG", "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if got := ClientLanguage(); got != i18n.French {
		t.Fatalf("ClientLanguage with an unreadable config and LANG=fr = %s, want fr", got)
	}
	if !strings.Contains(logs.String(), "config.json unreadable") {
		t.Fatalf("unreadable config logged nothing: %q", logs.String())
	}
}
