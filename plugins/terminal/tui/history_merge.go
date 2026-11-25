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
	for _, line := range lines {
		if line.Notice != nil {
			continue
		}
		out = append(out, transcriptLine{turnID: line.TurnID, role: line.Role, text: line.Text})
	}
	return out
}

func interruptionRecorded(history []contracts.ScrollbackLine, turnID string) bool {
	for index := range history {
		if notice := history[index].Notice; notice != nil && notice.Code == contracts.CodeTurnInterrupted && history[index].TurnID == turnID {
			return true
		}
	}
	return false
}

func noticedInHistory(event contracts.Event, history []contracts.ScrollbackLine) bool {
	return event.T == eventwire.EventReply && event.Outcome == contracts.TurnInterrupted && event.Text == "" && interruptionRecorded(history, event.TurnID)
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
		for earlier := reply - 1; earlier >= 0 && pending[earlier].Event.T != eventwire.EventReply; earlier-- {
			if pending[earlier].Event.T == eventwire.EventChunk && pending[earlier].Event.TurnID == pending[reply].Event.TurnID {
				skip[earlier] = true
			}
		}
	}
	out := make([]RoutedEvent, 0, len(pending))
	for index := range pending {
		if !skip[index] && !noticedInHistory(pending[index].Event, history) {
			out = append(out, pending[index])
		}
	}
	return out
}
