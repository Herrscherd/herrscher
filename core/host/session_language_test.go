package host

import (
	"path/filepath"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func languageStore(t *testing.T, sessions ...state.Session) *state.State {
	t.Helper()
	st, err := state.LoadState(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	for i := range sessions {
		if err := st.AddSession(sessions[i]); err != nil {
			t.Fatalf("AddSession(%q): %v", sessions[i].Name, err)
		}
	}
	return st
}

func TestBuilderRepliesFollowTheTargetSessionLanguage(t *testing.T) {
	st := languageStore(t, state.Session{Name: "fr-s", Language: "fr"})
	in := contracts.Input{Args: map[string]string{paramSession: "fr-s", "mode": "bypass"}}
	got, err := setApprovalMode(st, i18n.English, in)
	if err != nil {
		t.Fatalf("setApprovalMode: %v", err)
	}
	want := clitext.For(i18n.French).Text(clitext.HostApproveModeSet, clitext.Params{clitext.ParamName: "fr-s", clitext.ParamMode: "bypass"})
	if got != want {
		t.Fatalf("reply to an english operator about a french session = %q, want %q", got, want)
	}
}

func TestNamedSessionLanguageFallsBackToTheOperator(t *testing.T) {
	st := languageStore(t,
		state.Session{Name: "fr-s", Language: "fr"},
		state.Session{Name: "en-s", Language: "en"},
		state.Session{Name: "bare-s"},
	)
	cases := []struct {
		name     string
		session  string
		operator i18n.Language
		want     i18n.Language
	}{
		{"french session with english operator", "fr-s", i18n.English, i18n.French},
		{"english session with french operator", "en-s", i18n.French, i18n.English},
		{"unknown session follows the operator", "ghost", i18n.French, i18n.French},
		{"session without language follows the operator", "bare-s", i18n.French, i18n.French},
		{"zero operator resolves to english", "ghost", 0, i18n.English},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := namedSessionLanguage(st, tc.session, tc.operator); got != tc.want {
				t.Fatalf("namedSessionLanguage(%q, %v) = %v, want %v", tc.session, tc.operator, got, tc.want)
			}
		})
	}
}

func TestHandoffBriefDirectiveFollowsTheSessionLanguage(t *testing.T) {
	st := languageStore(t,
		state.Session{Name: "fr-s", Language: "fr"},
		state.Session{Name: "en-s", Language: "en"},
		state.Session{Name: "bare-s"},
	)
	if got := handoffBriefTaskFor(st, "fr-s"); !strings.HasSuffix(got, " Write the brief in French.") {
		t.Fatalf("french session brief task = %q, want the French directive", got)
	}
	if got := handoffBriefTaskFor(st, "en-s"); !strings.HasSuffix(got, " Write the brief in English.") {
		t.Fatalf("english session brief task = %q, want the English directive", got)
	}
	for _, name := range []string{"bare-s", "ghost"} {
		if got := handoffBriefTaskFor(st, name); got != handoffBriefTask {
			t.Fatalf("session %q without language brief task = %q, want no directive", name, got)
		}
	}
	if got := briefLanguageDirective(0); got != "" {
		t.Fatalf("zero language directive = %q, want none", got)
	}
}
