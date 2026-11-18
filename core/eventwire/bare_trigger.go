package eventwire

import (
	"encoding/json"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// IsBareTrigger reports whether value carries the bare trigger an older host puts in place of a JSON payload.
func IsBareTrigger(value string) bool {
	return !strings.HasPrefix(strings.TrimSpace(value), "{")
}

func decodeCompactionPayload[T any](value string, fromTrigger func(trigger string) T) (T, error) {
	var payload T
	text := strings.TrimSpace(value)
	if IsBareTrigger(text) {
		trigger, err := contracts.ParseCompactTrigger(text)
		if err != nil {
			return payload, fmt.Errorf("read bare compaction trigger: %w", err)
		}
		return fromTrigger(trigger.String()), nil
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return payload, fmt.Errorf("read compaction payload: %w", err)
	}
	return payload, nil
}
