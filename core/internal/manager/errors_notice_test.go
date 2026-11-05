package manager

import (
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

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
	if !errors.Is(ErrNoHome, ErrNoHome) || ErrNoHome.Error() != "no home set; run `set home` first" {
		t.Fatalf("ErrNoHome text or identity changed: %q", ErrNoHome.Error())
	}
	if got, want := renderNotice(t, i18n.French, ErrNoHome), "aucun accueil défini ; lance d'abord `set home`"; got != want {
		t.Fatalf("fr = %q, want %q", got, want)
	}
}

func TestTechnicalErrorKeepsEveryCause(t *testing.T) {
	err := &technicalError{msg: "restart failed; rollback failed", causes: []error{ErrNoHome, ErrSessionLimit}}
	if !errors.Is(err, ErrNoHome) || !errors.Is(err, ErrSessionLimit) {
		t.Fatal("technicalError must expose every cause to errors.Is")
	}
	if got := renderNotice(t, i18n.French, err); got != "erreur inattendue : restart failed; rollback failed" {
		t.Fatalf("fr = %q", got)
	}
}
