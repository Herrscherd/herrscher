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
	out := make(map[string][]string, len(declaredKeys()))
	for key, fields := range declaredKeys() {
		out[string(key)] = fields
	}
	return out
}

func TestEveryDeclaredKeyHasItsTextInEveryLanguage(t *testing.T) {
	i18ntest.CheckDeclared(t, declaredByName(), sources(t))
}

func TestEveryKeyFileIsDeclaredOnce(t *testing.T) {
	groups := []map[Key][]string{usageKeys(), cliKeys(), serveKeys(), sessionKeys(), pluginKeys()}
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	if total != len(declaredKeys()) {
		t.Fatalf("keys declared %d times for %d distinct keys: a key sits in two groups", total, len(declaredKeys()))
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

func TestRenderedMessagesInEachLanguage(t *testing.T) {
	cases := []struct {
		name   string
		key    Key
		params Params
		en     string
		fr     string
	}{
		{"attach no session", AttachNoSession, nil, "herrscher: no session is bound to this tab any more", "herrscher : aucune session n'est plus liée à cet onglet"},
		{"error line", ErrorLine, Params{"Message": "boom"}, "herrscher: boom", "herrscher : boom"},
		{"unknown command", CommandUnknown, Params{"Name": `"x"`}, `unknown command "x"`, `commande inconnue "x"`},
		{"env set failed", EnvSetFailed, Params{"Key": "A", "Err": "bad"}, "set A: bad", "définition de A : bad"},
		{"attach not available", AttachNotAvailable, Params{"Verb": `"agent"`}, `command "agent" is not available from the terminal (allowed: session, agent)`, `commande "agent" indisponible depuis le terminal (autorisées : session, agent)`},
		{"skill duplicate", SkillDuplicate, Params{"Name": "demo", "Kind": "bundle", "Owner": "core"}, "herrscher: skill demo from plugin bundle was left out: plugin core already ships one by that name", "herrscher : la compétence demo du plugin bundle a été écartée : le plugin core en livre déjà une de ce nom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := Text(i18n.English, tc.key, tc.params); err != nil || got != tc.en {
				t.Fatalf("en = %q, %v; want %q", got, err, tc.en)
			}
			if got, err := Text(i18n.French, tc.key, tc.params); err != nil || got != tc.fr {
				t.Fatalf("fr = %q, %v; want %q", got, err, tc.fr)
			}
		})
	}
}
