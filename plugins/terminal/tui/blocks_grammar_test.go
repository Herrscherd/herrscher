package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestSpinnerHintCarriesVerbDurationAndEscape(t *testing.T) {
	m := sizedTestModel(t, 74)
	tb := &tab{busy: true, startedAt: time.Now().Add(-12 * time.Second)}
	got := ansi.Strip(m.spinnerHint(tb))
	for _, want := range []string{newTranslator().t("tui.common.working", nil), "12s", "to interrupt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("spinner hint %q missing %q", got, want)
		}
	}
}
