package eventwire

import (
	"encoding/json"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// CompactionDone is the payload of an EventCompactionDone, carried JSON encoded in Value: the trigger, and the Notice (its code and arguments) of a success the gateway must render itself. Notice is nil when the success needs no relayed notice.
type CompactionDone struct {
	Trigger string            `json:"trigger"`
	Notice  *contracts.Notice `json:"notice,omitempty"`
}

// Encode returns the JSON form of the payload, the value of an EventCompactionDone.
func (done CompactionDone) Encode() (string, error) {
	encoded, err := json.Marshal(done)
	if err != nil {
		return "", fmt.Errorf("encode compaction done: %w", err)
	}
	return string(encoded), nil
}

// DecodeCompactionDone reads the payload carried in the value of an EventCompactionDone, or the bare trigger an older host sends.
func DecodeCompactionDone(value string) (CompactionDone, error) {
	if !strings.HasPrefix(value, "{") {
		return CompactionDone{Trigger: value}, nil
	}
	var done CompactionDone
	if err := json.Unmarshal([]byte(value), &done); err != nil {
		return CompactionDone{}, fmt.Errorf("decode compaction done: %w", err)
	}
	return done, nil
}
