package host

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestSessionLangStoresTheLanguageAndRepliesInIt(t *testing.T) {
	st := languageStore(t, state.Session{Name: "alpha"})
	got, err := setSessionLang(st, i18n.English, "alpha", "fr")
	if err != nil {
		t.Fatalf("setSessionLang: %v", err)
	}
	want := clitext.For(i18n.French).Text(clitext.HostSessionLangSet, clitext.Params{clitext.ParamName: "alpha", clitext.ParamLang: "fr"})
	if got != want {
		t.Fatalf("reply = %q, want the french %q", got, want)
	}
	sess, ok := st.FindSession("alpha")
	if !ok || sess.Language != "fr" {
		t.Fatalf("stored language = %q, want fr", sess.Language)
	}
}

func TestSessionLangAutoClearsTheSessionAndRepliesInTheOperatorLanguage(t *testing.T) {
	st := languageStore(t, state.Session{Name: "alpha", Language: "fr"})
	got, err := setSessionLang(st, i18n.French, "alpha", "auto")
	if err != nil {
		t.Fatalf("setSessionLang: %v", err)
	}
	want := clitext.For(i18n.French).Text(clitext.HostSessionLangReset, clitext.Params{clitext.ParamName: "alpha", clitext.ParamLang: "fr"})
	if got != want {
		t.Fatalf("reply = %q, want the french %q", got, want)
	}
	if sess, _ := st.FindSession("alpha"); sess.Language != "" {
		t.Fatalf("stored language = %q, want it cleared to follow the operator", sess.Language)
	}
}

func TestSessionLangVerbRunsThroughTheCommand(t *testing.T) {
	st := languageStore(t, state.Session{Name: "alpha"})
	cmd := sessionLangVerb(st, fixedLanguage(i18n.English))
	in := contracts.Input{Args: map[string]string{paramName: "alpha", paramLang: "fr"}}
	if _, err := cmd.Run(context.Background(), in); err != nil {
		t.Fatalf("session lang alpha fr: %v", err)
	}
	if sess, _ := st.FindSession("alpha"); sess.Language != "fr" {
		t.Fatalf("stored language = %q after the command, want fr", sess.Language)
	}
}

func TestSessionLangRefusesAnythingOutsideTheClosedSetInTheSessionLanguage(t *testing.T) {
	st := languageStore(t, state.Session{Name: "alpha", Language: "fr"})
	_, err := setSessionLang(st, i18n.English, "alpha", "klingon")
	if !errors.Is(err, errUnsupportedLang) {
		t.Fatalf("setSessionLang(klingon) = %v, want errUnsupportedLang", err)
	}
	phrase, textErr := clitext.Text(i18n.French, clitext.Key("cli.host.fault.unsupported_lang"), nil)
	if textErr != nil {
		t.Fatal(textErr)
	}
	got := localizeHostError(commandErrorLanguage(st, i18n.English, contracts.Input{Args: map[string]string{paramName: "alpha"}}), err).Error()
	if !strings.Contains(got, phrase) || strings.Contains(got, errUnsupportedLang.Error()) {
		t.Fatalf("error for a french session = %q, want the french phrase %q", got, phrase)
	}
}

func TestLocalizeHostErrorTranslatesEverySentinelInOneError(t *testing.T) {
	err := fmt.Errorf("%w; %w", errNoPendingChoice, errUnsupportedLang)
	got := localizeHostError(i18n.French, err).Error()
	for _, sentinel := range []error{errNoPendingChoice, errUnsupportedLang} {
		if strings.Contains(got, sentinel.Error()) {
			t.Fatalf("%q still carries the english sentinel %q", got, sentinel.Error())
		}
	}
	for _, key := range []string{"cli.host.fault.no_pending_choice", "cli.host.fault.unsupported_lang"} {
		phrase, textErr := clitext.Text(i18n.French, clitext.Key(key), nil)
		if textErr != nil {
			t.Fatal(textErr)
		}
		if !strings.Contains(got, phrase) {
			t.Fatalf("%q does not carry the french phrase of %s (%q)", got, key, phrase)
		}
	}
}

func TestCommandErrorsUseTheTargetSessionLanguage(t *testing.T) {
	st := languageStore(t, state.Session{Name: "fr-s", Language: "fr"}, state.Session{Name: "bare"})
	cases := []struct {
		name     string
		args     map[string]string
		operator i18n.Language
		want     i18n.Language
	}{
		{"targeted french session wins over an english operator", map[string]string{paramName: "fr-s"}, i18n.English, i18n.French},
		{"session without language follows the operator", map[string]string{paramName: "bare"}, i18n.French, i18n.French},
		{"no session named follows the operator", make(map[string]string), i18n.French, i18n.French},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := contracts.Input{Args: tc.args}
			if got := commandErrorLanguage(st, tc.operator, in); got != tc.want {
				t.Fatalf("commandErrorLanguage = %v, want %v", got, tc.want)
			}
		})
	}
}
