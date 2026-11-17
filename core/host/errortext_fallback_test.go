package host

import (
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

type outerWithoutCauseText struct{ cause error }

func (outerWithoutCauseText) Error() string { return "outer" }

func (e outerWithoutCauseText) Unwrap() error { return e.cause }

func TestErrorTextFallsBackToEnglishWhenTheLocalizedPartIsNotInTheText(t *testing.T) {
	cause := wrapHost(newHostMessages(), errors.New("cause"), "warn.health_server", map[string]any{detailParam: "x"})
	err := outerWithoutCauseText{cause: cause}
	if got := errorTextIn(newHostMessages(), i18n.French, err); got != "outer" {
		t.Fatalf("errorTextIn(French) = %q, want the english text %q unchanged", got, "outer")
	}
}
