package manage

import (
	"bytes"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestMsgLogsAMessageThatFailsToRender(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	text := msg(clitext.Key("cli.nowhere"), nil)

	if text != "cli.nowhere" {
		t.Fatalf("msg on an undeclared key = %q, want the key itself", text)
	}
	if !strings.Contains(logs.String(), "render message") || !strings.Contains(logs.String(), "cli.nowhere") {
		t.Fatalf("msg did not log the failure: %q", logs.String())
	}
}

func TestWritingPluginRowsLoadsTheCatalogOnce(t *testing.T) {
	rows := make([]PluginVersion, 200)
	for i := range rows {
		rows[i] = PluginVersion{Module: fmt.Sprintf("example.com/m%d", i), Installed: "v1.0.0", Latest: "v1.1.0", Pinned: true}
	}
	var out bytes.Buffer
	allocs := testing.AllocsPerRun(3, func() {
		out.Reset()
		if err := writePluginRows(&out, rows); err != nil {
			t.Fatal(err)
		}
	})
	const oneLoadBudget = 150000
	if allocs > oneLoadBudget {
		t.Fatalf("writing %d rows allocated %.0f times, above the %d of one catalogue load: the catalogue is reloaded per row", len(rows), allocs, oneLoadBudget)
	}
}
