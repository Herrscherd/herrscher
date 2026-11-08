package historywire

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestHistoryRoundTripsThroughTheWire(t *testing.T) {
	want := contracts.History{Messages: []contracts.HistoryMessage{
		{Role: contracts.RoleUser, Text: "lis le fichier"},
		{Role: contracts.RoleAssistant, Text: "voilà", Calls: []contracts.HistoryCall{
			{ID: "c1", Tool: "Read", Input: `{"path":"a.go"}`, Result: "package a", ResultIsError: true},
		}},
	}}
	frames, err := History(want)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(frames) != 1 || frames[0].T != EventHistory || !frames[0].Done {
		t.Fatalf("frames = %+v, want one final history frame", frames)
	}
	got, err := DecodeHistory(frames)
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
	frame, err := Prune("7", edits)
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

func TestAHistoryLargerThanAWireLineCrossesTheControlWire(t *testing.T) {
	result := strings.Repeat("<é\n\x01", 400_000)
	want := contracts.History{Messages: []contracts.HistoryMessage{{Role: contracts.RoleAssistant, Calls: []contracts.HistoryCall{{ID: "c1", Tool: "Read", Result: result}}}}}
	frames, err := History(want)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var wire bytes.Buffer
	for _, frame := range frames {
		if writeErr := control.WriteEvent(&wire, Answer(Read("9"), frame)); writeErr != nil {
			t.Fatalf("WriteEvent: %v", writeErr)
		}
	}
	var collector Collector
	complete := false
	err = control.ScanEvents(&wire, func(event contracts.Event) error {
		if event.Who != "9" {
			t.Fatalf("frame call id = %q, want 9", event.Who)
		}
		var addErr error
		complete, addErr = collector.Add(event)
		return addErr
	})
	if err != nil {
		t.Fatalf("ScanEvents over %d frames: %v", len(frames), err)
	}
	got, err := collector.History()
	if err != nil || !complete {
		t.Fatalf("History = %v, complete = %v", err, complete)
	}
	if !slices.EqualFunc(got.Messages, want.Messages, sameMessage) {
		t.Fatal("the history changed across the wire")
	}
}

func TestChunksNeverCutARune(t *testing.T) {
	text := strings.Repeat("a", ChunkLimit-1) + "é" + strings.Repeat("b", 10)
	end := chunkEnd(text)
	if end != ChunkLimit-1 {
		t.Fatalf("chunkEnd = %d, want %d, before the rune that straddles the limit", end, ChunkLimit-1)
	}
	frames, err := History(contracts.History{Messages: []contracts.HistoryMessage{{Role: contracts.RoleUser, Text: strings.Repeat("é", ChunkLimit)}}})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	for i, frame := range frames {
		if !utf8.ValidString(frame.Text) {
			t.Fatalf("frame %d of %d is not valid UTF-8", i, len(frames))
		}
	}
}

func TestHistoryPastTheLimitIsRefusedByItsSender(t *testing.T) {
	_, err := History(contracts.History{Messages: []contracts.HistoryMessage{{Role: contracts.RoleUser, Text: strings.Repeat("a", HistoryLimit)}}})
	if !errors.Is(err, ErrHistoryTooLarge) {
		t.Fatalf("History = %v, want ErrHistoryTooLarge", err)
	}
	if !errors.Is(DecodeFailure(Failure(err)), ErrHistoryTooLarge) {
		t.Fatal("the refusal loses ErrHistoryTooLarge across the wire")
	}
}

func TestCollectorRefusesAHistoryPastTheLimit(t *testing.T) {
	var collector Collector
	piece := contracts.Event{T: EventHistory, Text: strings.Repeat("a", ChunkLimit)}
	for range HistoryLimit / ChunkLimit {
		if _, err := collector.Add(piece); err != nil {
			t.Fatalf("Add under the limit: %v", err)
		}
	}
	if _, err := collector.Add(piece); !errors.Is(err, ErrHistoryTooLarge) {
		t.Fatalf("Add past the limit = %v, want ErrHistoryTooLarge", err)
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

func TestPruneRefusesEditsPastAFrame(t *testing.T) {
	_, err := Prune("1", []contracts.HistoryEdit{{CallID: strings.Repeat("c", ChunkLimit), Action: contracts.PruneCall}})
	if !errors.Is(err, ErrHistoryTooLarge) {
		t.Fatalf("Prune = %v, want ErrHistoryTooLarge", err)
	}
}
