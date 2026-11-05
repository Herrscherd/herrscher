package manage

import (
	"bytes"
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
