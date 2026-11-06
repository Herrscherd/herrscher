package language

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func silentLog() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestPickTakesTheFirstSourceThatNamesALanguage(t *testing.T) {
	log, _ := silentLog()
	got := Pick(log, Source{Origin: "config", Value: ""}, Source{Origin: "env", Value: "fr_FR.UTF-8"}, Source{Origin: "LANG", Value: "en"})
	if got != i18n.French {
		t.Fatalf("Pick = %v, want French from the env source", got)
	}
}

func TestPickFallsBackToEnglishWhenNothingIsSet(t *testing.T) {
	log, buf := silentLog()
	if got := Pick(log); got != i18n.English {
		t.Fatalf("Pick = %v, want English", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("no source, no warning expected, got %q", buf.String())
	}
}

func TestPickWarnsOnceAndSkipsAnInvalidValue(t *testing.T) {
	log, buf := silentLog()
	got := Pick(log, Source{Origin: "config", Value: "klingon"}, Source{Origin: "env", Value: "de"}, Source{Origin: "LANG", Value: "fr"})
	if got != i18n.French {
		t.Fatalf("Pick = %v, want the next valid source", got)
	}
	if n := strings.Count(buf.String(), "language ignored"); n != 1 {
		t.Fatalf("warnings = %d, want exactly one: %q", n, buf.String())
	}
}

func TestPickIsSilentOnAQuietLocale(t *testing.T) {
	log, buf := silentLog()
	if got := Pick(log, Source{Origin: "LANG", Value: "de_DE.UTF-8", Quiet: true}); got != i18n.English {
		t.Fatalf("Pick = %v, want English for an untranslated locale", got)
	}
	if buf.Len() != 0 {
		t.Fatalf("quiet source must not warn, got %q", buf.String())
	}
}

func TestOperatorIgnoresTheLocale(t *testing.T) {
	log, _ := silentLog()
	t.Setenv(EnvVar, "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	if got := Operator(log, ""); got != i18n.English {
		t.Fatalf("Operator = %v, want English: the daemon does not read $LANG", got)
	}
}

func TestOperatorReadsEnvThenConfig(t *testing.T) {
	log, _ := silentLog()
	t.Setenv(EnvVar, "en")
	if got := Operator(log, "fr"); got != i18n.English {
		t.Fatalf("Operator = %v, want the env value to win over config", got)
	}
	t.Setenv(EnvVar, "")
	if got := Operator(log, "fr"); got != i18n.French {
		t.Fatalf("Operator = %v, want the config value when env is unset", got)
	}
}

func TestClientOrderIsEnvThenConfigThenLocaleThenEnglish(t *testing.T) {
	cases := []struct {
		name, env, configured, locale string
		want                          i18n.Language
	}{
		{"env beats config and locale", "fr", "en", "de_DE.UTF-8", i18n.French},
		{"config beats locale", "", "fr", "de_DE.UTF-8", i18n.French},
		{"locale when nothing else names a language", "", "", "fr_FR.UTF-8", i18n.French},
		{"English when nothing names a language", "", "", "", i18n.English},
		{"English for an untranslated locale", "", "", "de_DE.UTF-8", i18n.English},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, _ := silentLog()
			t.Setenv(EnvVar, tc.env)
			t.Setenv("LANG", tc.locale)
			if got := Client(log, tc.configured); got != tc.want {
				t.Fatalf("Client = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestClientReadsLocaleWhenNothingElseIsSet(t *testing.T) {
	log, _ := silentLog()
	t.Setenv(EnvVar, "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	if got := Client(log, ""); got != i18n.French {
		t.Fatalf("Client = %v, want French from $LANG", got)
	}
}

func TestExportPublishesTheResolvedLanguageForThePlugins(t *testing.T) {
	t.Setenv(EnvVar, "")
	if err := Export(i18n.French); err != nil {
		t.Fatal(err)
	}
	log, _ := silentLog()
	if got := Operator(log, ""); got != i18n.French {
		t.Fatalf("after Export, Operator = %v, want French", got)
	}
}

func TestResolveWarnsOnceForAnInvalidConfigLanguage(t *testing.T) {
	log, buf := silentLog()
	t.Setenv(EnvVar, "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	langs := Resolve(log, "klingon", "")
	if langs.Operator != i18n.English || langs.Client != i18n.French {
		t.Fatalf("Resolve = %+v, want English operator and French client from LANG", langs)
	}
	if n := strings.Count(buf.String(), "language ignored"); n != 1 {
		t.Fatalf("warnings = %d, want exactly one: %s", n, buf.String())
	}
}

func TestExplicitOnlyForASupportedValue(t *testing.T) {
	t.Setenv(EnvVar, "")
	if Explicit("") || Explicit("klingon") {
		t.Fatal("nothing named, or an unsupported value, is not explicit")
	}
	if !Explicit("fr") {
		t.Fatal("a supported config value is explicit")
	}
	t.Setenv(EnvVar, "fr")
	if !Explicit("") {
		t.Fatal("a supported HERRSCHER_LANG is explicit")
	}
}

func TestForClientFollowsLocaleWithoutExportingIt(t *testing.T) {
	t.Setenv(EnvVar, "")
	t.Setenv("LANG", "fr_FR.UTF-8")
	if got := ForClient(""); got != i18n.French {
		t.Fatalf("ForClient = %v, want French from LANG", got)
	}
	if got := os.Getenv(EnvVar); got != "" {
		t.Fatalf("ForClient published %q, want nothing", got)
	}
}

func TestResolveKeepsTheConfiguredDefaultApartFromTheStoredSetting(t *testing.T) {
	t.Setenv(EnvVar, "")
	log, _ := silentLog()
	langs := Resolve(log, "en", "fr")
	if langs.Operator != i18n.French {
		t.Fatalf("operator = %v, want French from the stored setting", langs.Operator)
	}
	if langs.Default != i18n.English {
		t.Fatalf("default = %v, want English from config.json", langs.Default)
	}
}
