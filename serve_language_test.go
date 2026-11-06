package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/language"
)

func clearPublishedLanguage(t *testing.T) {
	t.Helper()
	t.Setenv(language.EnvVar, "")
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
}

func storeOperatorLanguage(t *testing.T, lang string) {
	t.Helper()
	body := `{"operator_lang":"` + lang + `"}`
	if err := os.WriteFile(filepath.Join(os.Getenv("HERRSCHER_STATE_DIR"), "state.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareLanguagesHonorsTheOperatorSetLang(t *testing.T) {
	clearPublishedLanguage(t)
	storeOperatorLanguage(t, "fr")
	langs, err := prepareLanguages(config.Config{Language: "en"}, host.DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.French {
		t.Fatalf("operator = %v, want French from /set lang", langs.Operator)
	}
	if langs.Default != i18n.English {
		t.Fatalf("default = %v, want the configured English", langs.Default)
	}
	if got := os.Getenv(language.EnvVar); got != "fr" {
		t.Fatalf("published %s = %q, want fr so plugins follow", language.EnvVar, got)
	}
}

func TestPrepareLanguagesEnvironmentStillBeatsTheOperatorSetLang(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv(language.EnvVar, "en")
	storeOperatorLanguage(t, "fr")
	langs, err := prepareLanguages(config.Config{}, host.DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.English {
		t.Fatalf("operator = %v, want English from %s", langs.Operator, language.EnvVar)
	}
}

func TestPrepareLanguagesKeepsTheClientLocaleOutOfTheDaemon(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "fr_FR.UTF-8")
	langs, err := prepareLanguages(config.Config{}, host.DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if langs.Client != i18n.French {
		t.Fatalf("client = %v, want French from LANG", langs.Client)
	}
	if langs.Operator != i18n.English {
		t.Fatalf("operator = %v, want English: the daemon does not read LANG", langs.Operator)
	}
	if got := os.Getenv(language.EnvVar); got != "" {
		t.Fatalf("%s = %q, want nothing published: the daemon would inherit the client locale", language.EnvVar, got)
	}
}

func TestPrepareLanguagesPublishesTheConfiguredLanguage(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "en_US.UTF-8")
	langs, err := prepareLanguages(config.Config{Language: "fr"}, host.DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.French {
		t.Fatalf("operator = %v, want French from config.json", langs.Operator)
	}
	if got := os.Getenv(language.EnvVar); got != "fr" {
		t.Fatalf("%s = %q, want fr before any plugin loads", language.EnvVar, got)
	}
}

func TestPrepareLanguagesKeepsAPublishedLanguageWithoutConfig(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv(language.EnvVar, "fr")
	langs, err := prepareLanguages(config.Config{}, host.DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.French {
		t.Fatalf("operator = %v, want French from HERRSCHER_LANG", langs.Operator)
	}
	if got := os.Getenv(language.EnvVar); got != "fr" {
		t.Fatalf("%s = %q, want fr kept", language.EnvVar, got)
	}
}

func TestPrepareLanguagesDefaultsToEnglishWithoutPublishing(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "")
	langs, err := prepareLanguages(config.Config{}, host.DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.English {
		t.Fatalf("operator = %v, want English", langs.Operator)
	}
	if got := os.Getenv(language.EnvVar); got != "" {
		t.Fatalf("%s = %q, want nothing published without an explicit language", language.EnvVar, got)
	}
}

func TestPrepareLanguagesWarnsOnceForAnInvalidHerrscherLang(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv(language.EnvVar, "klingon")
	t.Setenv("LANG", "")
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	langs, err := prepareLanguages(config.Config{}, host.DefaultStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.English {
		t.Fatalf("operator = %v, want English", langs.Operator)
	}
	if n := strings.Count(out.String(), "language ignored"); n != 1 {
		t.Fatalf("warnings = %d, want exactly one for an invalid %s:\n%s", n, language.EnvVar, out.String())
	}
}

func TestPrepareLanguagesWarnsOnceThroughTheProcessLogger(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "fr_FR.UTF-8")
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if _, err := prepareLanguages(config.Config{Language: "klingon"}, host.DefaultStatePath()); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "language ignored"); n != 1 {
		t.Fatalf("warnings = %d, want exactly one for the operator value:\n%s", n, out.String())
	}
}

func TestPrepareLanguagesReadsTheStateFileInUse(t *testing.T) {
	clearPublishedLanguage(t)
	custom := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(custom, []byte(`{"operator_lang":"fr"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	storeOperatorLanguage(t, "en")
	langs, err := prepareLanguages(config.Config{Language: "en"}, custom)
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.French {
		t.Fatalf("operator = %v, want French from the -state file in use, not the default state", langs.Operator)
	}
}
