package host

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"unicode"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/health"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

func TestProbeFailureTextReachesTheTableNeutralised(t *testing.T) {
	report := unreachableReport(errors.New("cli exited: \x1b]8;;https://evil.test\a\x1b[2J"))
	snapshot := health.Snapshot{Checks: []health.Check{{Group: health.GroupBackends, Name: "claude", Report: report}}}
	data, err := health.Encode(snapshot)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	decoded, err := health.Decode(data)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	var out bytes.Buffer
	if err := decoded.Table(&out, clitext.NewMessages(), i18n.English, termpaint.Palette{}); err != nil {
		t.Fatalf("Table() error = %v", err)
	}
	for _, r := range out.String() {
		if r != '\n' && unicode.IsControl(r) {
			t.Fatalf("Table() output carries the raw control %U:\n%q", r, out.String())
		}
	}
	if want := `\x1b]8;;https://evil.test\a\x1b[2J`; !strings.Contains(out.String(), want) {
		t.Errorf("Table() output does not show the reason as %q:\n%s", want, out.String())
	}
}
