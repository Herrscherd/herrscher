package host

import (
	"context"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/historywire"
)

func TestTooManyEditsReachesTheOperatorInFrenchWithoutTheEnglishText(t *testing.T) {
	f, _, _ := historyFixture(t, true)
	_, err := bridgeHistory{driver: f.driver}.PruneHistory(context.Background(), numberedEdits(historywire.MaxPruneEdits+1))
	if err == nil {
		t.Fatal("PruneHistory accepted more edits than one frame carries")
	}
	localized := localizeHostError(newHostMessages(), i18n.French, err).Error()
	want, textErr := clitext.Text(i18n.French, clitext.Key("cli.host.fault.too_many_edits"), nil)
	if textErr != nil {
		t.Fatalf("French text of cli.host.fault.too_many_edits: %v", textErr)
	}
	if localized != want {
		t.Fatalf("localized error = %q, want the French phrase %q", localized, want)
	}
	if strings.Contains(localized, errTooManyEdits.Error()) {
		t.Fatalf("localized error = %q, still carries the English text %q", localized, errTooManyEdits.Error())
	}
}
