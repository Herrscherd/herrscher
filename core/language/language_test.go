package language

import (
	"bytes"
	"log/slog"
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

func TestOperatorReadsConfigThenEnv(t *testing.T) {
	log, _ := silentLog()
	t.Setenv(EnvVar, "en")
	if got := Operator(log, "fr"); got != i18n.French {
		t.Fatalf("Operator = %v, want the config value to win", got)
	}
	if got := Operator(log, ""); got != i18n.English {
		t.Fatalf("Operator = %v, want the env value", got)
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
