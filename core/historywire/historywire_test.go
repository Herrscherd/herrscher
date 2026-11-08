package historywire

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestHistoryRoundTripsThroughTheWire(t *testing.T) {
	want := contracts.History{Messages: []contracts.HistoryMessage{
		{Role: contracts.RoleUser, Text: "lis le fichier"},
		{Role: contracts.RoleAssistant, Text: "voilà", Calls: []contracts.HistoryCall{
			{ID: "c1", Tool: "Read", Input: `{"path":"a.go"}`, Result: "package a", ResultIsError: true},
		}},
	}}
	frame, err := History(want)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if frame.T != EventHistory {
		t.Fatalf("frame type = %q, want %q", frame.T, EventHistory)
	}
	got, err := DecodeHistory(frame)
	if err != nil {
		t.Fatalf("DecodeHistory: %v", err)
	}
	if !slices.EqualFunc(got.Messages, want.Messages, sameMessage) {
		t.Fatalf("history = %+v, want %+v", got, want)
	}
}

func sameMessage(a, b contracts.HistoryMessage) bool {
	return a.Role == b.Role && a.Text == b.Text && slices.Equal(a.Calls, b.Calls)
}

func TestPruneAndOutcomeRoundTripThroughTheWire(t *testing.T) {
	edits := []contracts.HistoryEdit{{CallID: "c1", Action: contracts.PruneResult, KeepChars: 40}, {CallID: "c2", Action: contracts.PruneCall}}
	frame, err := Prune(edits)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	gotEdits, err := DecodePrune(frame)
	if err != nil {
		t.Fatalf("DecodePrune: %v", err)
	}
	if !slices.Equal(gotEdits, edits) {
		t.Fatalf("edits = %+v, want %+v", gotEdits, edits)
	}
	outcome := contracts.PruneOutcome{Edited: 2, CharsBefore: 900, CharsAfter: 120}
	frame, err = Pruned(outcome)
	if err != nil {
		t.Fatalf("Pruned: %v", err)
	}
	gotOutcome, err := DecodePruned(frame)
	if err != nil {
		t.Fatalf("DecodePruned: %v", err)
	}
	if gotOutcome != outcome {
		t.Fatalf("outcome = %+v, want %+v", gotOutcome, outcome)
	}
}

func TestUnknownActionDoesNotDecode(t *testing.T) {
	_, err := DecodePrune(contracts.Event{T: EventPrune, Text: `[{"call_id":"c1","action":"explode"}]`})
	if !errors.Is(err, ErrFrameMalformed) || !errors.Is(err, contracts.ErrUnknownPruneAction) {
		t.Fatalf("DecodePrune = %v, want a malformed frame naming the unknown action", err)
	}
}

func TestFailuresKeepTheirCauseAcrossTheWire(t *testing.T) {
	for _, cause := range []error{ErrNoHistoryEditor, contracts.ErrInvalidHistoryEdit, contracts.ErrUnknownHistoryCall} {
		sent := fmt.Errorf("bridge: %w", cause)
		got := DecodeFailure(Failure(sent))
		if !errors.Is(got, cause) {
			t.Fatalf("DecodeFailure(Failure(%v)) = %v, want it to match %v", sent, got, cause)
		}
		if got.Error() != sent.Error() {
			t.Fatalf("text = %q, want %q", got.Error(), sent.Error())
		}
	}
	if !errors.Is(DecodeFailure(NoPort()), ErrNoHistoryEditor) {
		t.Fatal("a no-port frame does not decode to ErrNoHistoryEditor")
	}
	plain := DecodeFailure(Failure(errors.New("disque plein")))
	if errors.Is(plain, ErrNoHistoryEditor) || errors.Is(plain, contracts.ErrInvalidHistoryEdit) || errors.Is(plain, contracts.ErrUnknownHistoryCall) {
		t.Fatalf("an untyped failure %v matches a typed cause", plain)
	}
}
