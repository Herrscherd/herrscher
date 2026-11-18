package cli

import (
	"errors"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestUsageErrorsAreRenderedInTheRequestedLanguage(t *testing.T) {
	t.Parallel()
	err := usageFailure(clitext.NewMessages(), errUnknownCommand, clitext.Params{clitext.ParamName: `"x"`})
	if !errors.Is(err, errUnknownCommand) {
		t.Fatal("a usage error must still match its sentinel")
	}
	if got := err.Error(); got != `unknown command "x"` {
		t.Fatalf("English rendering %q", got)
	}
	if got := Localize(clitext.NewMessages(), err, i18n.French).Error(); got != `commande inconnue "x"` {
		t.Fatalf("French rendering %q", got)
	}
	if !errors.Is(Localize(clitext.NewMessages(), err, i18n.French), errUnknownCommand) {
		t.Fatal("the localized usage error must keep matching its sentinel")
	}
}

func TestEveryUsageSentinelHasAKey(t *testing.T) {
	t.Parallel()
	messages := clitext.NewMessages()
	for _, cause := range []error{errUnknownCommand, errMissingRequired, errUnknownFlag, errFlagNeedsValue} {
		if usage, found := isUsage(usageFailure(messages, cause, nil)); !found || !errors.Is(usage.cause, cause) {
			t.Fatalf("%v has no usage key and would be shown raw", cause)
		}
	}
}
