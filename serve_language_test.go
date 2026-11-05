package main

import (
	"os"
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
	if got := os.Getenv(language.EnvVar); got != "en" {
		t.Fatalf("%s = %q, want the operator language only: the daemon would inherit the client locale", language.EnvVar, got)
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

func TestPrepareLanguagesDefaultsToEnglish(t *testing.T) {
	clearPublishedLanguage(t)
	t.Setenv("LANG", "")
	if _, err := prepareLanguages(config.Config{}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(language.EnvVar); got != "en" {
		t.Fatalf("%s = %q, want en", language.EnvVar, got)
	}
}
