package tui

import (
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestKeyedErrorRendersInTheLanguageOfTheReason(t *testing.T) {
	t.Parallel()
	err := keyedError{cause: errSizeLimit, key: "tui.attach.too_large", params: map[string]any{"Limit": 10}}
	if !errors.Is(err, errSizeLimit) {
		t.Fatal("a keyed error must still match its cause")
	}
	if got := err.Error(); got != "attachment too large: 10 MiB" {
		t.Fatalf("English rendering %q", got)
	}
	french := newTranslator().in(i18n.French)
	if got := localizedReason(french, err); got != "pièce jointe trop grande : 10 Mio" {
		t.Fatalf("French rendering %q", got)
	}
}
