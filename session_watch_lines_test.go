package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestFollowSessionPrintsOneStatusLineAndOneOutcomePerCall(t *testing.T) {
	out, err := followed(t, false,
		toolStart(1, "a"), toolDone(2, "a", time.Second, contracts.ToolCallSucceeded),
		toolStart(3, "b"), toolDone(4, "b", 2*time.Second, contracts.ToolCallSucceeded),
		replyDone(5), sessionClosed(6))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"  · read env.go",
		"  ✓ read env.go · " + clitext.FormatDuration(i18n.English, time.Second) + " · tokens: 0",
		"  · read env.go",
		"  ✓ read env.go · " + clitext.FormatDuration(i18n.English, 2*time.Second) + " · tokens: 0",
	}
	lines := strings.Split(out, "\n")
	if len(lines) < len(want) || !slices.Equal(lines[:len(want)], want) {
		t.Fatalf("output\n%q\nwant its first lines to be\n%q", out, want)
	}
}
