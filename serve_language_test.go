package main

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/config"
	"github.com/Herrscherd/herrscher/core/language"
)

func clearPublishedLanguage(t *testing.T) {
	t.Helper()
	t.Setenv(language.EnvVar, "")
}

func TestPrepareLanguagesKeepsTheClientLocaleOutOfTheDaemon(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "fr_FR.UTF-8")
	langs, err := prepareLanguages(config.Config{})
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
	langs, err := prepareLanguages(config.Config{Language: "fr"})
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
	langs, err := prepareLanguages(config.Config{})
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
	langs, err := prepareLanguages(config.Config{})
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

func TestPrepareLanguagesLeavesAnInvalidHerrscherLangToTheGateway(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv(language.EnvVar, "klingon")
	t.Setenv("LANG", "")
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	langs, err := prepareLanguages(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if langs.Operator != i18n.English {
		t.Fatalf("operator = %v, want English", langs.Operator)
	}
	if n := strings.Count(out.String(), "language ignored"); n != 0 {
		t.Fatalf("host warnings = %d, want none: the gateway reports an invalid %s itself:\n%s", n, language.EnvVar, out.String())
	}
}

func TestPrepareLanguagesWarnsOnceThroughTheProcessLogger(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "fr_FR.UTF-8")
	var out bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	if _, err := prepareLanguages(config.Config{Language: "klingon"}); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out.String(), "language ignored"); n != 1 {
		t.Fatalf("warnings = %d, want exactly one for the operator value:\n%s", n, out.String())
	}
}
