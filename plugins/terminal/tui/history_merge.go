package tui

import (
	"slices"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

const assistantRole = "assistant"

type transcriptLine struct {
	role string
	text string
}

type turnReply struct {
	turnID string
	text   string
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
	for shared := min(len(history), len(live)); shared > 0; shared-- {
		if slices.Equal(history[len(history)-shared:], live[:shared]) {
			return shared
		}
	}
	return 0
}

func answeredTurns(history []contracts.ScrollbackLine) map[turnReply]bool {
	answered := make(map[turnReply]bool)
	for _, ln := range history {
		if ln.Role == assistantRole && ln.TurnID != "" {
			answered[turnReply{turnID: ln.TurnID, text: ln.Text}] = true
		}
	}
	return answered
}

func untaggedTail(history []contracts.ScrollbackLine) []transcriptLine {
	start := len(history)
	for start > 0 && history[start-1].TurnID == "" {
		start--
	}
	return transcriptLines(history[start:])
}

func unseenPending(history []contracts.ScrollbackLine, pending []RoutedEvent) []RoutedEvent {
	answered := answeredTurns(history)
	var covered, untagged []int
	var untaggedLines []transcriptLine
	for index := range pending {
		line, ok := replyLine(pending[index])
		if !ok {
			continue
		}
		turnID := pending[index].Event.TurnID
		switch {
		case turnID == "":
			untagged = append(untagged, index)
			untaggedLines = append(untaggedLines, line)
		case answered[turnReply{turnID: turnID, text: line.text}]:
			covered = append(covered, index)
		}
	}
	covered = append(covered, untagged[:overlapLength(untaggedTail(history), untaggedLines)]...)
	skip := make([]bool, len(pending))
	for _, reply := range covered {
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
