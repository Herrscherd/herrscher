package main

import (
	"bytes"
	"context"
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
	langs, err := prepareLanguages(silentLogger(), config.Config{Language: "en"}, host.DefaultStatePath())
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
	langs, err := prepareLanguages(silentLogger(), config.Config{}, host.DefaultStatePath())
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
	langs, err := prepareLanguages(silentLogger(), config.Config{}, host.DefaultStatePath())
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
	langs, err := prepareLanguages(silentLogger(), config.Config{Language: "fr"}, host.DefaultStatePath())
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
	langs, err := prepareLanguages(silentLogger(), config.Config{}, host.DefaultStatePath())
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
	langs, err := prepareLanguages(silentLogger(), config.Config{}, host.DefaultStatePath())
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
	logger := bufferedLogger(&out)
	langs, err := prepareLanguages(logger, config.Config{}, host.DefaultStatePath())
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

func TestPrepareLanguagesWarnsOnceForAnInvalidConfigValue(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "fr_FR.UTF-8")
	var out bytes.Buffer
	logger := bufferedLogger(&out)
	if _, err := prepareLanguages(logger, config.Config{Language: "klingon"}, host.DefaultStatePath()); err != nil {
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
	langs, err := prepareLanguages(silentLogger(), config.Config{Language: "en"}, custom)
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.French {
		t.Fatalf("operator = %v, want French from the -state file in use, not the default state", langs.Operator)
	}
}

func TestStateFlagSelectsTheStateFileServeReads(t *testing.T) {
	clearPublishedLanguage(t)
	custom := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(custom, []byte(`{"operator_lang":"fr"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := stateFlagPath([]string{"-state", custom}); got != custom {
		t.Fatalf("stateFlagPath(-state) = %q, want %q", got, custom)
	}
	if got := stateFlagPath([]string{"--state=" + custom}); got != custom {
		t.Fatalf("stateFlagPath(--state=) = %q, want %q", got, custom)
	}
	if got := stateFlagPath(nil); got != host.DefaultStatePath() {
		t.Fatalf("stateFlagPath() = %q, want the default %q", got, host.DefaultStatePath())
	}
	langs, err := prepareLanguages(silentLogger(), config.Config{Language: "en"}, stateFlagPath([]string{"-state", custom}))
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.French {
		t.Fatalf("operator = %v, want French from the -state file", langs.Operator)
	}
}

func TestRunServeReadsTheLanguageFromTheStateFileItIsGiven(t *testing.T) {
	clearPublishedLanguage(t)
	custom := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(custom, []byte(`{"operator_lang":"fr"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var served i18n.Language
	capture := func(_ context.Context, _ host.RuntimeLayout, _ []string, _ config.Config, langs language.Languages) error {
		served = langs.Operator
		return nil
	}
	args := []string{"-config", filepath.Join(t.TempDir(), "absent.json"), "-state", custom}
	if err := runServe(context.Background(), testRuntime(t), args, capture); err != nil {
		t.Fatal(err)
	}
	if served != i18n.French {
		t.Fatalf("runServe served with operator %v, want French from the -state file", served)
	}
}

func bufferedLogger(out *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(out, nil))
}

func silentLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestPrepareLanguagesWarnsThroughTheInjectedLogger(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "")
	unreadable := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(unreadable, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := prepareLanguages(bufferedLogger(&out), config.Config{}, unreadable); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "operator language setting unreadable") {
		t.Fatalf("the injected logger missed the unreadable language setting:\n%s", out.String())
	}
}
