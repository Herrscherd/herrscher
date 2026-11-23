package clitext

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func finishedToolCall(state contracts.ToolCallState, tokens int, errText string) timeline.Call {
	elapsed := time.Second.Milliseconds()
	ended := time.Date(2026, 11, 23, 10, 0, 1, 0, time.UTC)
	return timeline.Call{ID: "a", Tool: "read", Detail: "env.go", State: state, Tokens: tokens, Error: errText, EndedAt: &ended, DurationMs: &elapsed}
}

func renderToolLine(t *testing.T, lang i18n.Language, call timeline.Call) string {
	t.Helper()
	key, params := ToolCallLine(lang, call)
	text, err := Text(lang, key, params)
	if err != nil {
		t.Fatalf("Text(%s) error: %v", key, err)
	}
	return text
}

func TestToolLineGroupsTheThousandsOfItsTokens(t *testing.T) {
	tests := []struct {
		name   string
		lang   i18n.Language
		tokens int
		want   string
	}{
		{name: "en one token", lang: i18n.English, tokens: 1, want: "tokens: 1"},
		{name: "en thousands", lang: i18n.English, tokens: 12345, want: "tokens: 12,345"},
		{name: "fr thousands", lang: i18n.French, tokens: 12345, want: "jetons : 12 345"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := renderToolLine(t, tc.lang, finishedToolCall(contracts.ToolCallSucceeded, tc.tokens, ""))
			if !strings.HasSuffix(line, " · "+tc.want) {
				t.Fatalf("line = %q, want it to end with %q", line, tc.want)
			}
		})
	}
}

func TestToolLineNeverShowsNegativeTokens(t *testing.T) {
	line := renderToolLine(t, i18n.English, finishedToolCall(contracts.ToolCallSucceeded, -12, ""))
	if !strings.HasSuffix(line, " · tokens: 0") {
		t.Fatalf("line = %q, want the tokens clamped to 0", line)
	}
}

func TestFailedToolLineWithoutReasonHasNoTrailingSeparator(t *testing.T) {
	key, _ := ToolCallLine(i18n.English, finishedToolCall(contracts.ToolCallFailed, 0, ""))
	if key != ToolFailedNoReason {
		t.Fatalf("key = %q, want %q for a failed call without an error", key, ToolFailedNoReason)
	}
	line := renderToolLine(t, i18n.English, finishedToolCall(contracts.ToolCallFailed, 0, ""))
	want := "  ✗ read env.go · " + FormatDuration(i18n.English, time.Second) + " · tokens: 0 · no reason given"
	if line != want {
		t.Fatalf("line = %q, want %q", line, want)
	}
}

func TestFailedToolLineWithoutReasonShowsItsTokens(t *testing.T) {
	tests := []struct {
		name   string
		lang   i18n.Language
		tokens string
		reason string
	}{
		{name: "en", lang: i18n.English, tokens: "tokens: 12,345", reason: "no reason given"},
		{name: "fr", lang: i18n.French, tokens: "jetons : 12 345", reason: "aucune raison donnée"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := renderToolLine(t, tc.lang, finishedToolCall(contracts.ToolCallFailed, 12345, ""))
			want := "  ✗ read env.go · " + FormatDuration(tc.lang, time.Second) + " · " + tc.tokens + " · " + tc.reason
			if line != want {
				t.Fatalf("line = %q, want %q", line, want)
			}
		})
	}
}

func TestFailedToolLineWithReasonShowsIt(t *testing.T) {
	key, params := ToolCallLine(i18n.English, finishedToolCall(contracts.ToolCallFailed, 0, "exit 2"))
	if key != ToolFailed {
		t.Fatalf("key = %q, want %q", key, ToolFailed)
	}
	if params[ParamError] != "exit 2" {
		t.Fatalf("error param = %v, want exit 2", params[ParamError])
	}
}

func TestFailedToolLineWithReasonShowsItsTokens(t *testing.T) {
	tests := []struct {
		name   string
		lang   i18n.Language
		tokens string
	}{
		{name: "en", lang: i18n.English, tokens: "tokens: 12,345"},
		{name: "fr", lang: i18n.French, tokens: "jetons : 12 345"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			line := renderToolLine(t, tc.lang, finishedToolCall(contracts.ToolCallFailed, 12345, "exit 2"))
			want := "  ✗ read env.go · " + FormatDuration(tc.lang, time.Second) + " · " + tc.tokens + " · exit 2"
			if line != want {
				t.Fatalf("line = %q, want %q", line, want)
			}
		})
	}
}
