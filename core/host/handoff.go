package host

import (
	"strconv"
	"strings"
)

// Coordination trailers: an agent signals an inter-session intent on a single
// line at the very end of its reply. done has priority over delegate over fanout
// over route over seal over merge over handoff when dispatched (see maybeCoordinate).
const (
	handoffMarker  = "⟢ handoff:"
	delegateMarker = "⟢ delegate:"
	doneMarker     = "⟢ done:"
	sealMarker     = "⟢ seal:"
	mergeMarker    = "⟢ merge:"
	fanoutMarker   = "⟢ fanout:"
	routeMarker    = "⟢ route:"
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

// parseMerge extracts a merge intent: "⟢ merge: <worker>". Like done and unlike
// handoff/delegate, the whole body is a single token (the worker name) — no
// em-dash split. An empty body is not a merge intent.
func parseMerge(reply string) (worker string, ok bool) {
	body, ok := parseTrailer(reply, mergeMarker)
	if !ok || body == "" {
		return "", false
	}
	return body, true
}

// parseSeal extracts a cohort-seal intent: "⟢ seal: <N>". The body is a single
// positive integer (the expected worker count); a non-integer, a non-positive
// value, or an empty body is not a seal.
func parseSeal(reply string) (n int, ok bool) {
	body, ok := parseTrailer(reply, sealMarker)
	if !ok || body == "" {
		return 0, false
	}
	v, err := strconv.Atoi(body)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

// splitAgentTasks splits "<agent> — <task1> ;; <task2> ;; …" into the agent and
// its task list: the em-dash separates agent from tasks, ";;" separates tasks.
// Empty tasks (extra ";;") are dropped. ok=false when the agent is empty or no
// non-empty task remains.
func splitAgentTasks(body string) (agent string, tasks []string, ok bool) {
	parts := strings.SplitN(body, "—", 2)
	if len(parts) != 2 {
		return "", nil, false
	}
	agent = strings.TrimSpace(parts[0])
	if agent == "" {
		return "", nil, false
	}
	for _, raw := range strings.Split(parts[1], ";;") {
		if t := strings.TrimSpace(raw); t != "" {
			tasks = append(tasks, t)
		}
	}
	if len(tasks) == 0 {
		return "", nil, false
	}
	return agent, tasks, true
}

// parseFanOut extracts a batch fan-out intent:
// "⟢ fanout: <agent> — <task1> ;; <task2> ;; …". One agent, one or more tasks.
// Returns ok=false when absent or malformed (empty agent, no non-empty task,
// missing em-dash).
func parseFanOut(reply string) (agent string, tasks []string, ok bool) {
	body, ok := parseTrailer(reply, fanoutMarker)
	if !ok {
		return "", nil, false
	}
	return splitAgentTasks(body)
}

// parseRoute extracts a routing intent: "⟢ route: <task>". Unlike delegate/handoff,
// NO agent is named — the host picks by capability match. The whole body is the
// task (no em-dash split); an empty body is not a route.
func parseRoute(reply string) (task string, ok bool) {
	body, ok := parseTrailer(reply, routeMarker)
	if !ok || body == "" {
		return "", false
	}
	return body, true
}
