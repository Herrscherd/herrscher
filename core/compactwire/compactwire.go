// Package compactwire is the wire form of a compaction between the host and its bridge.
package compactwire

import (
	"errors"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Event types and the NoCompactor code of the compaction wire.
const (
	EventRequest  = "compact"
	EventProgress = "compact_progress"
	EventResult   = "compact_result"
	EventError    = "compact_error"
	NoCompactor   = "no_compactor"
)

var errNoCompactorText = errors.New("backend implements no compaction")

// Request encodes a compaction request as the frame the host sends to the bridge.
func Request(req contracts.CompactRequest) (contracts.Event, error) {
	trigger, err := req.Trigger.MarshalText()
	if err != nil {
		return contracts.Event{}, err
	}
	return contracts.Event{
		T:        EventRequest,
		Value:    string(trigger),
		Text:     req.Instruction,
		Tokens:   req.TokensUsed,
		TokensIn: req.TokenLimit,
		Done:     req.UsageMeasured,
	}, nil
}

// DecodeRequest decodes a compaction request frame.
func DecodeRequest(frame contracts.Event) (contracts.CompactRequest, error) {
	var trigger contracts.CompactTrigger
	if err := trigger.UnmarshalText([]byte(frame.Value)); err != nil {
		return contracts.CompactRequest{}, err
	}
	return contracts.CompactRequest{
		Trigger:       trigger,
		UsageMeasured: frame.Done,
		TokensUsed:    frame.Tokens,
		TokenLimit:    frame.TokensIn,
		Instruction:   frame.Text,
	}, nil
}

// Progress encodes one progress line of a running compaction.
func Progress(line string) contracts.Event {
	return contracts.Event{T: EventProgress, Text: line}
}

// Result encodes the outcome of a compaction.
func Result(res contracts.CompactResult) contracts.Event {
	return contracts.Event{
		T:      EventResult,
		Text:   res.Note,
		Tokens: res.TokensAfter,
		Done:   res.TokensMeasured,
	}
}

// DecodeResult decodes the outcome of a compaction.
func DecodeResult(frame contracts.Event) contracts.CompactResult {
	return contracts.CompactResult{
		TokensAfter:    frame.Tokens,
		TokensMeasured: frame.Done,
		Note:           frame.Text,
	}
}

// Failure encodes a failed compaction.
func Failure(err error) contracts.Event {
	return contracts.Event{T: EventError, Text: err.Error()}
}

// NoPort encodes the refusal of an instance that does not implement Compactor.
func NoPort() contracts.Event {
	return contracts.Event{T: EventError, Value: NoCompactor, Text: errNoCompactorText.Error()}
}

// DecodeFailure returns the error carried by a failure frame.
func DecodeFailure(frame contracts.Event) error {
	return compactFailureError(frame.Text)
}

type compactFailureError string

func (e compactFailureError) Error() string {
	return string(e)
}
