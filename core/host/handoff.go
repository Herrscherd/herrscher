package host

import "strings"

// Coordination trailers: an agent signals an inter-session intent on a single
// line at the very end of its reply. done has priority over delegate over
// handoff when dispatched (see maybeCoordinate).
const (
	handoffMarker  = "⟢ handoff:"
	delegateMarker = "⟢ delegate:"
	doneMarker     = "⟢ done:"
)

// parseTrailer isolates the last non-empty line of reply and, if it starts with
// marker, returns the trimmed body. A trailer is always the reply's LAST line —
// a half-formed or mid-reply marker is never guessed at.
func parseTrailer(reply, marker string) (body string, ok bool) {
	lines := strings.Split(strings.TrimRight(reply, "\n \t"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, marker) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(last, marker)), true
}

// splitAgentTask splits "<agent> — <task>" on the em-dash; both must be non-empty.
func splitAgentTask(body string) (agent, task string, ok bool) {
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

// parseHandoff extracts a handoff intent: "⟢ handoff: <agent> — <task>".
// Returns ok=false when absent or malformed (empty agent/task, missing separator).
func parseHandoff(reply string) (agent, task string, ok bool) {
	body, ok := parseTrailer(reply, handoffMarker)
	if !ok {
		return "", "", false
	}
	return splitAgentTask(body)
}

// parseDelegate extracts a delegation intent: "⟢ delegate: <agent> — <task>".
// Same shape as handoff, distinct semantics (result-back: the lead stays alive,
// the worker records its parent).
func parseDelegate(reply string) (agent, task string, ok bool) {
	body, ok := parseTrailer(reply, delegateMarker)
	if !ok {
		return "", "", false
	}
	return splitAgentTask(body)
}

// parseDone extracts a completion report: "⟢ done: <summary>". No em-dash — the
// whole body is the summary (a worker may put dashes in it). An empty body is
// not a report.
func parseDone(reply string) (summary string, ok bool) {
	body, ok := parseTrailer(reply, doneMarker)
	if !ok || body == "" {
		return "", false
	}
	return body, true
}
