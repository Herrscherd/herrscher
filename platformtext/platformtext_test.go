package platformtext

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher-contracts/i18n/i18ntest"
)

func sources(t *testing.T) fs.FS {
	t.Helper()
	fsys, err := fs.Sub(locales, "locales")
	if err != nil {
		t.Fatal(err)
	}
	return fsys
}

func TestEveryPlatformKeyHasItsTextInEveryLanguage(t *testing.T) {
	declared := make(map[string][]string, len(Keys()))
	for _, key := range Keys() {
		declared[string(key)] = nil
	}
	i18ntest.CheckDeclared(t, declared, sources(t))
}

func TestEveryPlatformKeyRendersInEveryLanguage(t *testing.T) {
	for _, lang := range i18n.Languages() {
		tr := For(lang)
		for _, key := range Keys() {
			if text := tr.Text(key); text == string(key) {
				t.Errorf("%s: %q falls back to its key", lang, key)
			}
		}
		if err := tr.Err(); err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
	}
}

func TestTranslatorRefusesAnUnknownKey(t *testing.T) {
	tr := For(i18n.French)
	if got := tr.Text(Key("cli.nowhere")); got != "cli.nowhere" {
		t.Fatalf("unknown key rendered %q, want the key itself", got)
	}
	if !errors.Is(tr.Err(), ErrUndeclaredKey) {
		t.Fatalf("unknown key = %v, want ErrUndeclaredKey", tr.Err())
	}
}
