package tui

import (
	"slices"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

const assistantRole = "assistant"

type transcriptLine struct {
	turnID string
	role   string
	text   string
}

func transcriptLines(lines []contracts.ScrollbackLine) []transcriptLine {
	out := make([]transcriptLine, 0, len(lines))
	for _, ln := range lines {
		out = append(out, transcriptLine{turnID: ln.TurnID, role: ln.Role, text: ln.Text})
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
	return transcriptLine{turnID: routed.Event.TurnID, role: assistantRole, text: routed.Event.Text}, true
}

func sameLine(history, live transcriptLine) bool {
	if history.role != live.role || history.text != live.text {
		return false
	}
	return history.turnID == "" || live.turnID == "" || history.turnID == live.turnID
}

func overlapLength(history, live []transcriptLine) int {
	for shared := min(len(history), len(live)); shared > 0; shared-- {
		if slices.EqualFunc(history[len(history)-shared:], live[:shared], sameLine) {
			return shared
		}
	}
	return 0
}

func unseenPending(history []contracts.ScrollbackLine, pending []RoutedEvent) []RoutedEvent {
	var replies []int
	var lines []transcriptLine
	for index := range pending {
		if line, ok := replyLine(pending[index]); ok {
			replies = append(replies, index)
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
	for index := range pending {
		if !skip[index] {
			out = append(out, pending[index])
		}
	}
	return out
}
