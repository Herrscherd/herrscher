package manager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func historyFixture() []state.TranscriptEntry {
	return []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "first", TurnID: "turn_a"},
		{Ts: "t2", Role: "assistant", Text: "one", TurnID: "turn_a", TokensIn: 10, TokensOut: 4, DurMs: 1500, Cost: 0.01},
		{Ts: "t3", Role: "user", Text: "second", TurnID: "turn_b"},
		{Ts: "t4", Role: "notice", Kind: state.KindTurnInterrupted, TurnID: "turn_b"},
		{Ts: "t5", Role: "assistant", Text: "seeded", TurnID: "turn_c"},
	}
}

func historyJSON(t *testing.T, h *Handler, args map[string]string) []historyTurn {
	t.Helper()
	out, err := cli.Plain(h.sessionHistoryRun(context.Background(), contracts.Input{JSON: true, Args: args}))
	if err != nil {
		t.Fatalf("session history: %v", err)
	}
	var turns []historyTurn
	if err := json.Unmarshal([]byte(out), &turns); err != nil {
		t.Fatalf("output is not a JSON array: %v (%q)", err, out)
	}
	return turns
}

func TestSessionHistoryGroupsEntriesIntoTurns(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, historyFixture())
	turns := historyJSON(t, &Handler{partDir: dir}, map[string]string{"name": "alpha"})
	if len(turns) != 3 {
		t.Fatalf("want 3 turns, got %d: %+v", len(turns), turns)
	}
	first := turns[0]
	if first.TurnID != "turn_a" || first.Prompt != "first" || first.Reply != "one" || first.TokensIn != 10 || first.TokensOut != 4 || first.DurMs != 1500 {
		t.Fatalf("first turn = %+v", first)
	}
	if !turns[1].Interrupted || turns[1].Reply != "" {
		t.Fatalf("second turn should be interrupted without reply: %+v", turns[1])
	}
	if turns[2].TurnID != "turn_c" || turns[2].Prompt != "" || turns[2].Reply != "seeded" {
		t.Fatalf("a reply with its own turn id opens a turn: %+v", turns[2])
	}
}

func TestSessionHistorySinceKeepsTheTurnsAfter(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, historyFixture())
	turns := historyJSON(t, &Handler{partDir: dir}, map[string]string{"name": "alpha", "since": "turn_a"})
	if len(turns) != 2 || turns[0].TurnID != "turn_b" {
		t.Fatalf("since turn_a = %+v, want turn_b and turn_c", turns)
	}
	if latest := historyJSON(t, &Handler{partDir: dir}, map[string]string{"name": "alpha", "since": "turn_c"}); len(latest) != 0 {
		t.Fatalf("since the last turn = %+v, want nothing", latest)
	}
}

func TestSessionHistoryRefusesAnUnknownSince(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, historyFixture())
	h := &Handler{partDir: dir}
	_, err := h.sessionHistoryRun(context.Background(), contracts.Input{Args: map[string]string{"name": "alpha", "since": "turn_zz"}})
	var invalid *InvalidValueError
	if !errors.As(err, &invalid) || invalid.Value != "turn_zz" {
		t.Fatalf("err = %v, want an InvalidValueError on turn_zz", err)
	}
}

func TestSessionHistoryNumbersTurnsWithoutID(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, []state.TranscriptEntry{
		{Ts: "t1", Role: "user", Text: "a"},
		{Ts: "t2", Role: "assistant", Text: "b"},
		{Ts: "t3", Role: "user", Text: "c"},
	})
	turns := historyJSON(t, &Handler{partDir: dir}, map[string]string{"name": "alpha"})
	if len(turns) != 2 || turns[0].TurnID != "1" || turns[1].TurnID != "2" || turns[0].Reply != "b" {
		t.Fatalf("legacy transcript turns = %+v", turns)
	}
}

func TestSessionHistoryTextInTheCallerLanguage(t *testing.T) {
	dir := t.TempDir()
	seedTranscript(t, dir, historyFixture())
	h := &Handler{messages: clitext.NewMessages(), partDir: dir, lang: i18n.English}
	ctx := clitext.WithCallerLanguage(context.Background(), i18n.French.String())
	out, err := cli.Plain(h.sessionHistoryRun(ctx, contracts.Input{Args: map[string]string{"name": "alpha"}}))
	if err != nil {
		t.Fatalf("session history: %v", err)
	}
	for _, want := range []string{"tour turn_a", "› first", "one", "1,5 s", "en sortie"} {
		if !strings.Contains(out, want) {
			t.Fatalf("history text lacks %q:\n%s", want, out)
		}
	}
}

func TestSessionHistoryEmpty(t *testing.T) {
	h := &Handler{messages: clitext.NewMessages(), partDir: t.TempDir(), lang: i18n.English}
	if turns := historyJSON(t, h, map[string]string{"name": "ghost"}); len(turns) != 0 {
		t.Fatalf("unknown session = %+v, want an empty array", turns)
	}
	out, err := cli.Plain(h.sessionHistoryRun(context.Background(), contracts.Input{Args: map[string]string{"name": "ghost"}}))
	if err != nil || out != "No turn recorded yet." {
		t.Fatalf("empty history = %q, %v", out, err)
	}
}
