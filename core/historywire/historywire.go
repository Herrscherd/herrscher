// Package historywire is the wire form of a history edit between the host and its
// bridge: the host reads the conversation of a session and prunes tool calls from
// it while a compaction stage runs, and the bridge answers from its backend.
package historywire

import (
	"encoding/json"
	"errors"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Event types of the history wire, and the failure codes a history_error frame
// carries in its Value.
const (
	EventRead    = "history_read"
	EventHistory = "history"
	EventPrune   = "history_prune"
	EventPruned  = "history_pruned"
	EventError   = "history_error"
	NoEditor     = "no_history_editor"
	InvalidEdit  = "invalid_edit"
	UnknownCall  = "unknown_call"
)

// ErrNoHistoryEditor reports a backend instance that does not implement
// contracts.HistoryEditor. Match it with errors.Is.
var ErrNoHistoryEditor = errors.New("backend implements no history editor")

// ErrFrameMalformed reports a history frame whose payload cannot be decoded.
// Match it with errors.Is.
var ErrFrameMalformed = errors.New("history frame malformed")

type historyFrame struct {
	Messages []messageFrame `json:"messages"`
}

type messageFrame struct {
	Role  contracts.HistoryRole `json:"role"`
	Text  string                `json:"text,omitempty"`
	Calls []callFrame           `json:"calls,omitempty"`
}

type callFrame struct {
	ID            string `json:"id"`
	Tool          string `json:"tool"`
	Input         string `json:"input,omitempty"`
	Result        string `json:"result,omitempty"`
	ResultIsError bool   `json:"result_is_error,omitempty"`
}

type editFrame struct {
	CallID    string                `json:"call_id"`
	Action    contracts.PruneAction `json:"action"`
	KeepChars int                   `json:"keep_chars,omitempty"`
}

type outcomeFrame struct {
	Edited      int `json:"edited"`
	CharsBefore int `json:"chars_before"`
	CharsAfter  int `json:"chars_after"`
}

// Read encodes the request the host sends to read the history of the session.
func Read() contracts.Event {
	return contracts.Event{T: EventRead}
}

// History encodes the history a bridge answers a read with.
func History(history contracts.History) (contracts.Event, error) {
	frame := historyFrame{Messages: make([]messageFrame, 0, len(history.Messages))}
	for _, message := range history.Messages {
		calls := make([]callFrame, 0, len(message.Calls))
		for _, call := range message.Calls {
			calls = append(calls, callFrame(call))
		}
		frame.Messages = append(frame.Messages, messageFrame{Role: message.Role, Text: message.Text, Calls: calls})
	}
	text, err := encode(frame)
	if err != nil {
		return contracts.Event{}, err
	}
	return contracts.Event{T: EventHistory, Text: text}, nil
}

// DecodeHistory decodes the history a bridge answered a read with.
func DecodeHistory(event contracts.Event) (contracts.History, error) {
	var frame historyFrame
	if err := decode(event.Text, &frame); err != nil {
		return contracts.History{}, err
	}
	history := contracts.History{Messages: make([]contracts.HistoryMessage, 0, len(frame.Messages))}
	for _, message := range frame.Messages {
		calls := make([]contracts.HistoryCall, 0, len(message.Calls))
		for _, call := range message.Calls {
			calls = append(calls, contracts.HistoryCall(call))
		}
		if len(calls) == 0 {
			calls = nil
		}
		history.Messages = append(history.Messages, contracts.HistoryMessage{Role: message.Role, Text: message.Text, Calls: calls})
	}
	return history, nil
}

// Prune encodes the edits the host asks the bridge to apply.
func Prune(edits []contracts.HistoryEdit) (contracts.Event, error) {
	frames := make([]editFrame, 0, len(edits))
	for _, edit := range edits {
		frames = append(frames, editFrame(edit))
	}
	text, err := encode(frames)
	if err != nil {
		return contracts.Event{}, err
	}
	return contracts.Event{T: EventPrune, Text: text}, nil
}

// DecodePrune decodes the edits of a prune request.
func DecodePrune(event contracts.Event) ([]contracts.HistoryEdit, error) {
	var frames []editFrame
	if err := decode(event.Text, &frames); err != nil {
		return nil, err
	}
	edits := make([]contracts.HistoryEdit, 0, len(frames))
	for _, frame := range frames {
		edits = append(edits, contracts.HistoryEdit(frame))
	}
	return edits, nil
}

// Pruned encodes the outcome of applied edits.
func Pruned(outcome contracts.PruneOutcome) (contracts.Event, error) {
	text, err := encode(outcomeFrame(outcome))
	if err != nil {
		return contracts.Event{}, err
	}
	return contracts.Event{T: EventPruned, Text: text}, nil
}

// DecodePruned decodes the outcome of applied edits.
func DecodePruned(event contracts.Event) (contracts.PruneOutcome, error) {
	var frame outcomeFrame
	if err := decode(event.Text, &frame); err != nil {
		return contracts.PruneOutcome{}, err
	}
	return contracts.PruneOutcome(frame), nil
}

// Failure encodes a failed read or prune. An error matching
// contracts.ErrInvalidHistoryEdit or contracts.ErrUnknownHistoryCall travels with
// its code, so the host matches it again on the other side.
func Failure(err error) contracts.Event {
	return contracts.Event{T: EventError, Value: failureCode(err), Text: err.Error()}
}

// NoPort encodes the refusal of an instance that does not implement
// contracts.HistoryEditor.
func NoPort() contracts.Event {
	return contracts.Event{T: EventError, Value: NoEditor, Text: ErrNoHistoryEditor.Error()}
}

// DecodeFailure returns the error a failure frame carries. It matches
// ErrNoHistoryEditor, contracts.ErrInvalidHistoryEdit or
// contracts.ErrUnknownHistoryCall when the frame names that code.
func DecodeFailure(event contracts.Event) error {
	return failureError{text: event.Text, cause: causeOf(event.Value)}
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, ErrNoHistoryEditor):
		return NoEditor
	case errors.Is(err, contracts.ErrInvalidHistoryEdit):
		return InvalidEdit
	case errors.Is(err, contracts.ErrUnknownHistoryCall):
		return UnknownCall
	}
	return ""
}

func causeOf(code string) error {
	switch code {
	case NoEditor:
		return ErrNoHistoryEditor
	case InvalidEdit:
		return contracts.ErrInvalidHistoryEdit
	case UnknownCall:
		return contracts.ErrUnknownHistoryCall
	}
	return nil
}

type failureError struct {
	text  string
	cause error
}

func (e failureError) Error() string {
	return e.text
}

func (e failureError) Unwrap() error {
	return e.cause
}

func encode(payload any) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrFrameMalformed, err)
	}
	return string(encoded), nil
}

func decode(text string, payload any) error {
	if err := json.Unmarshal([]byte(text), payload); err != nil {
		return fmt.Errorf("%w: %w", ErrFrameMalformed, err)
	}
	return nil
}
