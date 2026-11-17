package tui

import (
	"slices"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

type transcriptLine struct {
	role string
	text string
}

func transcriptLines(lines []contracts.ScrollbackLine) []transcriptLine {
	out := make([]transcriptLine, 0, len(lines))
	for _, ln := range lines {
		out = append(out, transcriptLine{role: scrollbackRole(ln.Role), text: ln.Text})
	}
	return out
}

func replyLine(routed RoutedEvent) (transcriptLine, bool) {
	if routed.Event.T != eventwire.EventReply || routed.Event.Text == "" {
		return transcriptLine{}, false
	}
	if _, isErr := hostError(routed.Event.Text); isErr {
		return transcriptLine{}, false
	}
	return transcriptLine{role: roleAgent, text: routed.Event.Text}, true
}

func overlapLength(history, live []transcriptLine) int {
	for n := min(len(history), len(live)); n > 0; n-- {
		if slices.Equal(history[len(history)-n:], live[:n]) {
			return n
		}
	}
	return 0
}

func unseenPending(history []contracts.ScrollbackLine, pending []RoutedEvent) []RoutedEvent {
	var replies []int
	var lines []transcriptLine
	for i, routed := range pending {
		if line, ok := replyLine(routed); ok {
			replies = append(replies, i)
			lines = append(lines, line)
		}
	}
	covered := overlapLength(transcriptLines(history), lines)
	skip := make([]bool, len(pending))
	for _, reply := range replies[:covered] {
		skip[reply] = true
		for j := reply - 1; j >= 0 && pending[j].Event.T == eventwire.EventChunk; j-- {
			skip[j] = true
		}
	}
	out := make([]RoutedEvent, 0, len(pending))
	for i, routed := range pending {
		if !skip[i] {
			out = append(out, routed)
		}
	}
	return out
}
