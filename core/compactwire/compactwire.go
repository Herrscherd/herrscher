// Package compactwire is the wire form of a compaction between the host and its bridge.
package compactwire

import (
	"encoding/json"
	"errors"
	"fmt"

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

var (
	errNoCompactorText = errors.New("backend implements no compaction")
	errUnknownArgKind  = errors.New("unknown notice argument kind")
)

// ErrNoticeMalformed reports a compaction notice that cannot be decoded.
var ErrNoticeMalformed = errors.New("compaction notice malformed")

type noticeFrame struct {
	Code  uint16                  `json:"code"`
	Args  []argFrame              `json:"args,omitempty"`
	Stats *contracts.CompactStats `json:"stats,omitempty"`
}

type argFrame struct {
	Name  string          `json:"name"`
	Kind  uint8           `json:"kind"`
	Value json.RawMessage `json:"value"`
}

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

// Result encodes the outcome of a compaction. The stats of the stage travel in the value
// next to the notice, so a result without stats is the notice alone, as older backends
// send it.
func Result(res contracts.CompactResult) (contracts.Event, error) {
	value := ""
	if res.Notice.Code != 0 || res.Stats != nil {
		encoded, err := encodeFrame(res.Notice, res.Stats)
		if err != nil {
			return contracts.Event{}, err
		}
		value = encoded
	}
	return contracts.Event{
		T:      EventResult,
		Text:   res.Note,
		Value:  value,
		Tokens: res.TokensAfter,
		Done:   res.TokensMeasured,
	}, nil
}

// DecodeResult decodes the outcome of a compaction. A result that carries no stats decodes
// with nil Stats.
func DecodeResult(frame contracts.Event) (contracts.CompactResult, error) {
	res := contracts.CompactResult{
		TokensAfter:    frame.Tokens,
		TokensMeasured: frame.Done,
		Note:           frame.Text,
	}
	decoded, err := decodeFrame(frame.Value)
	if err != nil {
		return res, fmt.Errorf("%w: %w", ErrNoticeMalformed, err)
	}
	res.Notice = decoded.notice
	res.Stats = decoded.stats
	return res, nil
}

// EncodeNotice encodes a notice in the wire form shared by the compaction events and the
// command socket. A notice without a code encodes to the empty string.
func EncodeNotice(notice contracts.Notice) (string, error) {
	if notice.Code == 0 {
		return "", nil
	}
	return encodeFrame(notice, nil)
}

func encodeFrame(notice contracts.Notice, stats *contracts.CompactStats) (string, error) {
	frame := noticeFrame{Code: uint16(notice.Code), Args: make([]argFrame, 0, len(notice.Args)), Stats: stats}
	for _, arg := range notice.Args {
		value, err := json.Marshal(arg.Value())
		if err != nil {
			return "", fmt.Errorf("notice argument %q: %w", arg.Name(), err)
		}
		frame.Args = append(frame.Args, argFrame{Name: arg.Name(), Kind: uint8(arg.Kind()), Value: value})
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		return "", fmt.Errorf("compaction notice code %d: %w", notice.Code, err)
	}
	return string(encoded), nil
}

// DecodeNotice decodes a notice encoded by EncodeNotice. The empty string decodes to the
// zero notice.
func DecodeNotice(value string) (contracts.Notice, error) {
	decoded, err := decodeFrame(value)
	return decoded.notice, err
}

type decodedFrame struct {
	notice contracts.Notice
	stats  *contracts.CompactStats
}

func decodeFrame(value string) (decodedFrame, error) {
	if value == "" {
		return decodedFrame{}, nil
	}
	var frame noticeFrame
	if err := json.Unmarshal([]byte(value), &frame); err != nil {
		return decodedFrame{}, fmt.Errorf("compaction notice: %w", err)
	}
	args := make([]contracts.Arg, 0, len(frame.Args))
	for _, raw := range frame.Args {
		arg, err := decodeArg(raw)
		if err != nil {
			return decodedFrame{}, err
		}
		args = append(args, arg)
	}
	return decodedFrame{
		notice: contracts.Notice{Code: contracts.Code(frame.Code), Args: args},
		stats:  frame.Stats,
	}, nil
}

func decodeArg(frame argFrame) (contracts.Arg, error) {
	switch contracts.ArgKind(frame.Kind) {
	case contracts.ArgText:
		var text string
		if err := json.Unmarshal(frame.Value, &text); err != nil {
			return contracts.Arg{}, fmt.Errorf("notice argument %q: %w", frame.Name, err)
		}
		return contracts.Text(frame.Name, text), nil
	case contracts.ArgRef:
		var identifier string
		if err := json.Unmarshal(frame.Value, &identifier); err != nil {
			return contracts.Arg{}, fmt.Errorf("notice argument %q: %w", frame.Name, err)
		}
		return contracts.Ref(frame.Name, identifier), nil
	case contracts.ArgCount:
		var count int
		if err := json.Unmarshal(frame.Value, &count); err != nil {
			return contracts.Arg{}, fmt.Errorf("notice argument %q: %w", frame.Name, err)
		}
		return contracts.Count(count), nil
	}
	return contracts.Arg{}, fmt.Errorf("notice argument %q of kind %d: %w", frame.Name, frame.Kind, errUnknownArgKind)
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
