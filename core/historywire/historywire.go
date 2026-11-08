// Package historywire is the wire form of a history edit between the host and its
// bridge: the host reads the conversation of a session and prunes tool calls from
// it while a compaction stage runs, and the bridge answers from its backend.
package historywire

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Event types of the history wire.
const (
	EventRead    = "history_read"
	EventHistory = "history"
	EventPrune   = "history_prune"
	EventPruned  = "history_pruned"
	EventError   = "history_error"
)

// ChunkLimit is the most bytes of encoded history one history frame carries, so
// the frame stays under the line limit of the control wire once escaped again.
const ChunkLimit = 128 * 1024

// HistoryLimit is the most bytes of encoded history a bridge sends and a
// Collector accepts.
const HistoryLimit = 64 * 1024 * 1024

// PruneBatch is the most edits one prune frame carries, so the frame stays under
// the line limit of the control wire.
const PruneBatch = 1000

// FailureCode names the cause a history_error frame carries in its Value.
type FailureCode int

// The failure codes of a history_error frame.
const (
	FailureOther FailureCode = iota + 1
	FailureNoEditor
	FailureInvalidEdit
	FailureUnknownCall
	FailureTooLarge
)

// String returns the wire form of the code.
func (c FailureCode) String() string {
	switch c {
	case FailureNoEditor:
		return "no_history_editor"
	case FailureInvalidEdit:
		return "invalid_edit"
	case FailureUnknownCall:
		return "unknown_call"
	case FailureTooLarge:
		return "history_too_large"
	case FailureOther:
		return "other"
	}
	return "other"
}

// CodeOf returns the failure code of a history_error frame, FailureOther when
// its Value names no known code.
func CodeOf(event contracts.Event) FailureCode {
	for _, code := range []FailureCode{FailureNoEditor, FailureInvalidEdit, FailureUnknownCall, FailureTooLarge} {
		if event.Value == code.String() {
			return code
		}
	}
	return FailureOther
}

// ErrNoHistoryEditor reports a backend instance that does not implement
// contracts.HistoryEditor. Match it with errors.Is.
var ErrNoHistoryEditor = errors.New("backend implements no history editor")

// ErrFrameMalformed reports a history frame whose payload cannot be decoded.
// Match it with errors.Is.
var ErrFrameMalformed = errors.New("history frame malformed")

// ErrHistoryTooLarge reports a history whose encoding passes HistoryLimit.
// Match it with errors.Is.
var ErrHistoryTooLarge = errors.New("history too large")

// IsFrame reports whether an event type belongs to the history wire.
func IsFrame(eventType string) bool {
	switch eventType {
	case EventRead, EventHistory, EventPrune, EventPruned, EventError:
		return true
	}
	return false
}

// Answer stamps answer with the call id of the request it answers, so the host
// tells it from the late answer of an abandoned call.
func Answer(request, answer contracts.Event) contracts.Event {
	answer.Who = request.Who
	return answer
}

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

// Read encodes the request the host sends to read the history of the session,
// under the call id its answers carry back.
func Read(callID string) contracts.Event {
	return contracts.Event{T: EventRead, Who: callID}
}

// History encodes the history a bridge answers a read with, as frames of at most
// ChunkLimit bytes of text, the last one marked Done. It fails with
// ErrHistoryTooLarge when the encoding passes HistoryLimit.
func History(history contracts.History) ([]contracts.Event, error) {
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
		return nil, err
	}
	if len(text) > HistoryLimit {
		return nil, fmt.Errorf("%w: %d bytes, more than %d", ErrHistoryTooLarge, len(text), HistoryLimit)
	}
	var frames []contracts.Event
	for text != "" {
		piece := text[:chunkEnd(text)]
		text = text[len(piece):]
		frames = append(frames, contracts.Event{T: EventHistory, Text: piece, Done: text == ""})
	}
	return frames, nil
}

func chunkEnd(text string) int {
	if len(text) <= ChunkLimit {
		return len(text)
	}
	end := ChunkLimit
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return end
}

// Collector gathers the history frames of one read until the frame marked Done.
type Collector struct {
	text strings.Builder
}

// Add appends a history frame and reports whether it completes the history. It
// fails with ErrHistoryTooLarge once the gathered text passes HistoryLimit.
func (c *Collector) Add(event contracts.Event) (bool, error) {
	if c.text.Len()+len(event.Text) > HistoryLimit {
		return false, fmt.Errorf("%w: more than %d bytes", ErrHistoryTooLarge, HistoryLimit)
	}
	c.text.WriteString(event.Text)
	return event.Done, nil
}

// History decodes the gathered history.
func (c *Collector) History() (contracts.History, error) {
	var frame historyFrame
	if err := decode(c.text.String(), &frame); err != nil {
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

// DecodeHistory decodes the history a bridge answered a read with.
func DecodeHistory(frames []contracts.Event) (contracts.History, error) {
	var collector Collector
	for i := range frames {
		if _, err := collector.Add(frames[i]); err != nil {
			return contracts.History{}, err
		}
	}
	return collector.History()
}

// Prune encodes the edits the host asks the bridge to apply, under the call id
// its answer carries back. It fails with ErrHistoryTooLarge when the edits
// encode to more than ChunkLimit bytes.
func Prune(callID string, edits []contracts.HistoryEdit) (contracts.Event, error) {
	frames := make([]editFrame, 0, len(edits))
	for _, edit := range edits {
		frames = append(frames, editFrame(edit))
	}
	text, err := encode(frames)
	if err != nil {
		return contracts.Event{}, err
	}
	if len(text) > ChunkLimit {
		return contracts.Event{}, fmt.Errorf("%w: %d bytes of edits, more than %d", ErrHistoryTooLarge, len(text), ChunkLimit)
	}
	return contracts.Event{T: EventPrune, Who: callID, Text: text}, nil
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
	return contracts.Event{T: EventError, Value: failureCode(err).String(), Text: err.Error()}
}

// NoPort encodes the refusal of an instance that does not implement
// contracts.HistoryEditor.
func NoPort() contracts.Event {
	return contracts.Event{T: EventError, Value: FailureNoEditor.String(), Text: ErrNoHistoryEditor.Error()}
}

// DecodeFailure returns the error a failure frame carries. It matches
// ErrNoHistoryEditor, ErrHistoryTooLarge, contracts.ErrInvalidHistoryEdit or
// contracts.ErrUnknownHistoryCall when the frame names that code.
func DecodeFailure(event contracts.Event) error {
	return failureError{text: event.Text, cause: causeOf(CodeOf(event))}
}

func failureCode(err error) FailureCode {
	switch {
	case errors.Is(err, ErrNoHistoryEditor):
		return FailureNoEditor
	case errors.Is(err, contracts.ErrInvalidHistoryEdit):
		return FailureInvalidEdit
	case errors.Is(err, contracts.ErrUnknownHistoryCall):
		return FailureUnknownCall
	case errors.Is(err, ErrHistoryTooLarge):
		return FailureTooLarge
	}
	return FailureOther
}

func causeOf(code FailureCode) error {
	switch code {
	case FailureNoEditor:
		return ErrNoHistoryEditor
	case FailureInvalidEdit:
		return contracts.ErrInvalidHistoryEdit
	case FailureUnknownCall:
		return contracts.ErrUnknownHistoryCall
	case FailureTooLarge:
		return ErrHistoryTooLarge
	case FailureOther:
		return nil
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
