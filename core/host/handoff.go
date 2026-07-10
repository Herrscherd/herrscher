package host

import "strings"

// handoffMarker prefixes the single trailer line an agent uses to signal a relay.
const handoffMarker = "⟢ handoff:"

// parseHandoff extracts a handoff intent from an agent reply. The signal is a
// single trailer line, "⟢ handoff: <agent> — <task>", that MUST be the reply's
// last non-empty line — a half-formed or mid-reply marker is never guessed at.
// Returns ok=false when absent or malformed (empty agent/task, missing separator).
func parseHandoff(reply string) (agent, task string, ok bool) {
	lines := strings.Split(strings.TrimRight(reply, "\n \t"), "\n")
	if len(lines) == 0 {
		return "", "", false
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, handoffMarker) {
		return "", "", false
	}
	body := strings.TrimSpace(strings.TrimPrefix(last, handoffMarker))
	parts := strings.SplitN(body, "—", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	agent = strings.TrimSpace(parts[0])
	task = strings.TrimSpace(parts[1])
	if agent == "" || task == "" {
		return "", "", false
	}
	return agent, task, true
}
