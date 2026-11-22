package manager

import (
	"context"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/timeline"
)

func toolsStart() time.Time {
	return time.Date(2026, 11, 22, 10, 0, 0, 0, time.UTC)
}

func toolsFixture() []state.TranscriptEntry {
	durMs := int64(1500)
	end := toolsStart().Add(1500 * time.Millisecond)
	return []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "look", TurnID: "turn_t"},
		{Ts: "t2", Role: "assistant", Text: "found", TurnID: "turn_t", Tools: []timeline.Call{
			{ID: "a", Tool: "read", Detail: "env.go", State: contracts.ToolCallSucceeded, StartedAt: toolsStart(), EndedAt: &end, DurationMs: &durMs, Tokens: 3},
			{ID: "b", Tool: "grep", Detail: "TODO", State: contracts.ToolCallStarted, StartedAt: toolsStart()},
		}},
	}
}

func historyRaw(t *testing.T, h *Handler, args map[string]string) string {
	t.Helper()
	out, err := cli.Plain(h.sessionHistoryRun(context.Background(), contracts.Input{JSON: true, Args: args}))
	if err != nil {
		t.Fatalf("session history: %v", err)
	}
	return out
}

func TestSessionHistoryJSONCarriesEachToolCallOfATurn(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, toolsFixture())
	turns := historyJSON(t, &Handler{partDir: dir}, map[string]string{"name": "alpha"})
	if len(turns) != 1 || len(turns[0].Tools) != 2 {
		t.Fatalf("turns = %+v, want one turn with two tool calls", turns)
	}
	done, open := turns[0].Tools[0], turns[0].Tools[1]
	if done.ID != "a" || done.Tool != "read" || done.Detail != "env.go" || done.Tokens != 3 || done.DurationMs == nil || *done.DurationMs != 1500 {
		t.Fatalf("finished call = %+v", done)
	}
	if open.ID != "b" || open.Finished() {
		t.Fatalf("open call = %+v, want unfinished", open)
	}
}

func TestSessionHistoryJSONNamesTheToolFields(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, toolsFixture())
	raw := historyRaw(t, &Handler{partDir: dir}, map[string]string{"name": "alpha"})
	for _, want := range []string{`"tools":[`, `"id":"a"`, `"tool":"read"`, `"duration_ms":1500`, `"started_at"`, `"tokens":3`} {
		if !strings.Contains(raw, want) {
			t.Fatalf("json %s lacks %s", raw, want)
		}
	}
}

func TestSessionHistoryTextShowsEachToolCallLine(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, toolsFixture())
	h := &Handler{messages: clitext.NewMessages(), partDir: dir, lang: i18n.English}
	out, err := cli.Plain(h.sessionHistoryRun(context.Background(), contracts.Input{Args: map[string]string{"name": "alpha"}}))
	if err != nil {
		t.Fatalf("session history: %v", err)
	}
	finished := "✓ read env.go · " + clitext.FormatDuration(i18n.English, 1500*time.Millisecond) + " · 3 tokens"
	for _, want := range []string{finished, "… grep TODO · unfinished"} {
		if !strings.Contains(out, want) {
			t.Fatalf("history text lacks %q:\n%s", want, out)
		}
	}
}

func TestSessionHistoryKeepsAToolOnlyTurn(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "run", TurnID: "turn_q"},
		{Ts: "t2", Role: "assistant", TurnID: "turn_q", Tools: toolsFixture()[1].Tools},
	})
	turns := historyJSON(t, &Handler{partDir: dir}, map[string]string{"name": "alpha"})
	if len(turns) != 1 || len(turns[0].Tools) != 2 {
		t.Fatalf("turns = %+v, want the tool-only reply kept with its calls", turns)
	}
}
