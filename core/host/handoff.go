package host

import (
	"strconv"
	"strings"
)

const (
	handoffMarker    = "⟢ handoff:"
	delegateMarker   = "⟢ delegate:"
	doneMarker       = "⟢ done:"
	sealMarker       = "⟢ seal:"
	mergeMarker      = "⟢ merge:"
	fanoutMarker     = "⟢ fanout:"
	routeMarker      = "⟢ route:"
	trailerSeparator = "\u2014"
)

func parseTrailer(reply, marker string) (string, bool) {
	lines := strings.Split(strings.TrimRight(reply, "\n \t"), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if !strings.HasPrefix(last, marker) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(last, marker)), true
}

func splitAgentTask(body string) (string, string, bool) {
	parts := strings.SplitN(body, trailerSeparator, 2)
	if len(parts) != 2 {
		return "", "", false
	}
	agent := strings.TrimSpace(parts[0])
	task := strings.TrimSpace(parts[1])
	if agent == "" || task == "" {
		return "", "", false
	}
	return agent, task, true
}

func parseHandoff(reply string) (string, string, bool) {
	body, ok := parseTrailer(reply, handoffMarker)
	if !ok {
		return "", "", false
	}
	return splitAgentTask(body)
}

func parseDelegate(reply string) (string, string, bool) {
	body, ok := parseTrailer(reply, delegateMarker)
	if !ok {
		return "", "", false
	}
	return splitAgentTask(body)
}

func parseSingleToken(reply, marker string) (string, bool) {
	body, ok := parseTrailer(reply, marker)
	if !ok || body == "" {
		return "", false
	}
	return body, true
}

func parseDone(reply string) (string, bool) {
	return parseSingleToken(reply, doneMarker)
}

func parseMerge(reply string) (string, bool) {
	return parseSingleToken(reply, mergeMarker)
}

func parseSeal(reply string) (int, bool) {
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

func splitAgentTasks(body string) (string, []string, bool) {
	parts := strings.SplitN(body, trailerSeparator, 2)
	if len(parts) != 2 {
		return "", nil, false
	}
	agent := strings.TrimSpace(parts[0])
	if agent == "" {
		return "", nil, false
	}
	var tasks []string
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

func parseFanOut(reply string) (string, []string, bool) {
	body, ok := parseTrailer(reply, fanoutMarker)
	if !ok {
		return "", nil, false
	}
	return splitAgentTasks(body)
}

func parseRoute(reply string) (string, bool) {
	return parseSingleToken(reply, routeMarker)
}
