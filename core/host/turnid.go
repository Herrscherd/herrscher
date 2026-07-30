package host

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const maxTurnIDLength = 128

// TurnIDError reports a supplied seed turn_id that cannot be carried safely
// through argv and JSON-line protocol surfaces.
type TurnIDError struct {
	Reason string
}

func (e *TurnIDError) Error() string {
	return "invalid turn_id: " + e.Reason
}

func newTurnID() string {
	return "turn_" + rand.Text()
}

func resolveTurnID(value string, supplied bool) (string, error) {
	if !supplied {
		return newTurnID(), nil
	}
	if strings.TrimSpace(value) == "" {
		return "", &TurnIDError{Reason: "must not be empty"}
	}
	if strings.HasPrefix(value, "--") {
		return "", &TurnIDError{Reason: "must not look like a command flag"}
	}
	if len(value) > maxTurnIDLength {
		return "", &TurnIDError{Reason: fmt.Sprintf("must be at most %d bytes", maxTurnIDLength)}
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' || r == '~' {
			continue
		}
		return "", &TurnIDError{Reason: fmt.Sprintf("contains unsafe character %q", r)}
	}
	return value, nil
}
