package eventwire

import (
	"encoding/json"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Reasons a compaction failed that the host can name without a notice code. They travel in CompactionFailure.Reason.
const (
	CompactReasonBridgeGone         = "bridge_gone"
	CompactReasonNoCompactorFactory = "no_compactor_factory"
	CompactReasonNilCompactor       = "nil_compactor"
	CompactReasonNoCompactorPlugin  = "no_compactor_plugin"
	CompactReasonNoHistoryPort      = "no_history_port"
	CompactReasonHistoryTimeout     = "history_timeout"
	CompactReasonHistoryTooLarge    = "history_too_large"
	CompactReasonTooManyEdits       = "too_many_edits"
	CompactReasonUnknown            = "unknown"
)

// CompactionFailure is the payload of an EventCompactionFailed, carried JSON encoded in Value: the trigger, then either a Notice (its code and arguments, in the form contracts encodes) or a Reason.
type CompactionFailure struct {
	Trigger string            `json:"trigger"`
	Notice  *contracts.Notice `json:"notice,omitempty"`
	Reason  string            `json:"reason,omitempty"`
}

// Encode returns the JSON form of the payload, the value of an EventCompactionFailed.
func (failure CompactionFailure) Encode() (string, error) {
	encoded, err := json.Marshal(failure)
	if err != nil {
		return "", fmt.Errorf("encode compaction failure: %w", err)
	}
	return string(encoded), nil
}

// DecodeCompactionFailure reads the payload carried in the value of an EventCompactionFailed.
func DecodeCompactionFailure(value string) (CompactionFailure, error) {
	var failure CompactionFailure
	if err := json.Unmarshal([]byte(value), &failure); err != nil {
		return CompactionFailure{}, fmt.Errorf("decode compaction failure: %w", err)
	}
	return failure, nil
}
