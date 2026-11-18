package eventwire

import (
	"encoding/json"
	"fmt"
)

// CompactionDone is the payload of an EventCompactionDone, carried JSON encoded in Value: the trigger, and the notice Code of a success the gateway must render itself. Code is zero when the success needs no relayed notice.
type CompactionDone struct {
	Trigger string `json:"trigger"`
	Code    uint16 `json:"code,omitempty"`
}

// Encode returns the JSON form of the payload, the value of an EventCompactionDone.
func (done CompactionDone) Encode() (string, error) {
	encoded, err := json.Marshal(done)
	if err != nil {
		return "", fmt.Errorf("encode compaction done: %w", err)
	}
	return string(encoded), nil
}

// DecodeCompactionDone reads the payload carried in the value of an EventCompactionDone.
func DecodeCompactionDone(value string) (CompactionDone, error) {
	var done CompactionDone
	if err := json.Unmarshal([]byte(value), &done); err != nil {
		return CompactionDone{}, fmt.Errorf("decode compaction done: %w", err)
	}
	return done, nil
}
