package clitext

import (
	"errors"
	"io/fs"
	"strings"
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

func declaredByName() map[string][]string {
	out := make(map[string][]string, len(declared))
	for key, fields := range declared {
		out[string(key)] = fields
	}
	return out
}

func TestEveryDeclaredKeyHasItsTextInEveryLanguage(t *testing.T) {
	i18ntest.CheckDeclared(t, declaredByName(), sources(t))
}

func TestEveryKeyFileIsDeclaredOnce(t *testing.T) {
	groups := []map[Key][]string{usageKeys, serviceKeys, serveKeys, bridgeKeys, manageKeys, accountKeys, sessionKeys, pluginKeys}
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total != len(declared) {
		t.Fatalf("keys declared %d times for %d distinct keys: a key sits in two groups", total, len(declared))
	}
}

func TestTextRefusesAnUndeclaredKey(t *testing.T) {
	_, err := Text(i18n.English, Key("cli.nowhere"), nil)
	if !errors.Is(err, ErrUndeclaredKey) {
		t.Fatalf("Text on an undeclared key = %v, want ErrUndeclaredKey", err)
	}
}

func TestTextRefusesParamsThatDoNotMatchTheKey(t *testing.T) {
	_, err := Text(i18n.English, UsageTagline, Params{"Unused": "x"})
	if !errors.Is(err, ErrParamsMismatch) {
		t.Fatalf("Text with an undeclared param = %v, want ErrParamsMismatch", err)
	}
}

func TestTranslatorKeepsTheFirstFailure(t *testing.T) {
	tr := For(i18n.French)
	got := tr.Text(Key("cli.nowhere"), nil)
	_ = tr.Text(Key("cli.elsewhere"), nil)
	if got != "cli.nowhere" {
		t.Fatalf("Text after a failure = %q, want the key itself", got)
	}
	if !errors.Is(tr.Err(), ErrUndeclaredKey) || !strings.Contains(tr.Err().Error(), "cli.nowhere") {
		t.Fatalf("Err = %v, want the first failure, on cli.nowhere", tr.Err())
	}
}
