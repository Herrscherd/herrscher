package manager

import (
	"errors"
	"fmt"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func describeArgs(args []contracts.Arg) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, fmt.Sprintf("%s/%d=%v", a.Name(), a.Kind(), a.Value()))
	}
	return out
}

func TestManagerNoticesCarryTheirCodeArgsAndEnglishText(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		text     string
		wantCode contracts.Code
		wantArgs []string
	}{
		{
			name:     "shared agent",
			err:      &SharedAgentError{Agent: "revue"},
			text:     `session create with agent "revue" cannot be shared: an agent needs a place of its own, and shared:true runs in the main checkout`,
			wantCode: contracts.CodeSharedAgent,
			wantArgs: []string{"Agent/1=revue"},
		},
		{
			name:     "unknown approval mode",
			err:      &UnknownApprovalModeError{Mode: "yolo"},
			text:     `unknown approval mode "yolo": ask, accept-edits, plan, auto, bypass or strict`,
			wantCode: contracts.CodeUnknownApprovalMode,
			wantArgs: []string{"Mode/1=yolo"},
		},
		{
			name:     "unsupported home type",
			err:      &UnsupportedHomeTypeError{Type: "thread"},
			text:     `home type "thread" unsupported`,
			wantCode: contracts.CodeUnsupportedHomeType,
			wantArgs: []string{"Type/1=thread"},
		},
		{
			name:     "channel kind under",
			err:      &ChannelKindError{Channel: "123", Kind: "voice", Need: "x"},
			text:     `channel 123 is "voice": x`,
			wantCode: contracts.CodeChannelKindUnder,
			wantArgs: []string{"Channel/3=123", "Kind/1=voice"},
		},
		{
			name:     "channel kind home",
			err:      &ChannelKindError{Channel: "123", Kind: "voice", Need: "x", Home: true},
			text:     `channel 123 is "voice": x`,
			wantCode: contracts.CodeChannelKindHome,
			wantArgs: []string{"Channel/3=123", "Kind/1=voice"},
		},
		{
			name:     "dirty worktree close",
			err:      newDirtyWorktreeError("s1", errors.New("worktree has uncommitted changes")),
			text:     "worktree has uncommitted changes; commit, or close with force:true to discard (branch session/s1 is kept)",
			wantCode: contracts.CodeSessionCloseDirty,
			wantArgs: []string{"Name/1=s1"},
		},
		{
			name:     "model required",
			err:      ErrModelRequired,
			text:     "a model is required under the gateway-only route policy: without one the session would spawn on this machine's own vendor login",
			wantCode: contracts.CodeModelRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.text {
				t.Fatalf("Error() = %q, want %q", got, tc.text)
			}
			notice, ok := errors.AsType[contracts.NoticeError](tc.err)
			if !ok {
				t.Fatalf("%v carries no notice", tc.err)
			}
			got := notice.Notice()
			if got.Code != tc.wantCode {
				t.Fatalf("code = %s, want %s", got.Code.Key(), tc.wantCode.Key())
			}
			if fmt.Sprint(describeArgs(got.Args)) != fmt.Sprint(tc.wantArgs) {
				t.Fatalf("args = %v, want %v", describeArgs(got.Args), tc.wantArgs)
			}
		})
	}
}

func renderNotice(t *testing.T, lang i18n.Language, err error) string {
	t.Helper()
	noticed, ok := errors.AsType[contracts.NoticeError](err)
	if !ok {
		t.Fatalf("%v carries no notice", err)
	}
	text, renderErr := contracts.NewRenderer().Render(lang, noticed.Notice())
	if renderErr != nil {
		t.Fatalf("render %v: %v", err, renderErr)
	}
	return text
}

func TestSessionLimitKeepsItsEnglishTextAndItsSentinel(t *testing.T) {
	err := &SessionLimitError{Max: 3}
	if got, want := err.Error(), "session limit reached (3): close a session before creating another"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(err, ErrSessionLimit) {
		t.Fatal("SessionLimitError must still match ErrSessionLimit")
	}
	if got, want := renderNotice(t, i18n.French, err), "limite de sessions atteinte : 3 sessions autorisées ; ferme-en une avant d'en créer une autre"; got != want {
		t.Fatalf("fr = %q, want %q", got, want)
	}
	if got, want := renderNotice(t, i18n.English, err), "session limit reached: 3 sessions allowed; close one before creating another"; got != want {
		t.Fatalf("en = %q, want %q", got, want)
	}
}

func TestInvalidValueWithoutHintUsesTheHintlessCode(t *testing.T) {
	err := &InvalidValueError{Field: "terminal_only", Value: "maybe"}
	if got := err.Notice().Code; got != contracts.CodeInvalidValue {
		t.Fatalf("code = %s, want %s", got.Key(), contracts.CodeInvalidValue.Key())
	}
	if got, want := renderNotice(t, i18n.French, err), "valeur invalide maybe pour terminal_only"; got != want {
		t.Fatalf("fr = %q, want %q", got, want)
	}
}

func TestInvalidValueWithHintKeepsTheHint(t *testing.T) {
	err := &InvalidValueError{Field: "terminal_only", Value: "maybe", Hint: "use true or false"}
	if got := err.Notice().Code; got != contracts.CodeInvalidValueHint {
		t.Fatalf("code = %s, want %s", got.Key(), contracts.CodeInvalidValueHint.Key())
	}
	if got, want := renderNotice(t, i18n.French, err), "valeur invalide maybe pour terminal_only : use true or false"; got != want {
		t.Fatalf("fr = %q, want %q", got, want)
	}
}

func TestChannelKindCodeFollowsThePlacement(t *testing.T) {
	under := &ChannelKindError{Channel: "123", Kind: "voice", Need: "x"}
	if got := under.Notice().Code; got != contracts.CodeChannelKindUnder {
		t.Fatalf("under code = %s", got.Key())
	}
	home := &ChannelKindError{Channel: "123", Kind: "voice", Need: "x", Home: true}
	if got := home.Notice().Code; got != contracts.CodeChannelKindHome {
		t.Fatalf("home code = %s", got.Key())
	}
}

func TestSentinelsKeepTheirIdentityAndRenderInFrench(t *testing.T) {
	if ErrNoHome.Error() != "no home set; run `set home` first" {
		t.Fatalf("ErrNoHome text changed: %q", ErrNoHome.Error())
	}
	if got, want := renderNotice(t, i18n.French, ErrNoHome), "aucun accueil défini ; lance d'abord `set home`"; got != want {
		t.Fatalf("fr = %q, want %q", got, want)
	}
}

func TestLocalizedErrorKeepsEveryCause(t *testing.T) {
	err := &localizedError{english: "restart failed; rollback failed", text: "red\u00e9marrage échoué ; retour arrière échoué", causes: []error{ErrNoHome, ErrSessionLimit}}
	if !errors.Is(err, ErrNoHome) || !errors.Is(err, ErrSessionLimit) {
		t.Fatal("localizedError must expose every cause to errors.Is")
	}
	if got := err.Error(); got != "restart failed; rollback failed" {
		t.Fatalf("Error() = %q, want the English text", got)
	}
	if got := renderNotice(t, i18n.French, err); got != "erreur inattendue : red\u00e9marrage échoué ; retour arrière échoué" {
		t.Fatalf("fr = %q", got)
	}
}

func TestLocalizedManagerErrorRendersInTheOperatorLanguage(t *testing.T) {
	cause := errors.New("disk full")
	h := &Handler{lang: i18n.French}
	err := h.labelled("worktree", clitext.HostErrWorktree, cause)
	if err.Error() != "worktree: disk full" {
		t.Fatalf("Error() = %q, want the English text", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatalf("the cause must stay reachable through errors.Is")
	}
	localized, ok := err.(*localizedError)
	if !ok {
		t.Fatalf("the error is %T, want a localizedError", err)
	}
	notice := localized.Notice()
	want := clitext.For(i18n.French).Text(clitext.HostErrWorktree, clitext.Params{clitext.ParamErr: "disk full"})
	if len(notice.Args) != 1 || fmt.Sprint(notice.Args[0].Value()) != want {
		t.Fatalf("notice detail = %v, want %q", notice.Args, want)
	}
	if want == "worktree: disk full" {
		t.Fatalf("the French text must differ from the English one: %q", want)
	}
}
