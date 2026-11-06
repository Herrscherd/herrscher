package manager

import (
	"fmt"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

const handoffSummaryTail = 12

func buildHandoffSeed(entries []state.TranscriptEntry, mode string) string {
	if len(entries) == 0 {
		return ""
	}
	elided := 0
	if mode == "summary" && len(entries) > handoffSummaryTail {
		elided = len(entries) - handoffSummaryTail
		entries = entries[elided:]
	}
	var text strings.Builder
	text.WriteString("[Conversation resumed after a model change. Context of the earlier thread below; continue it.]\n")
	if elided > 0 {
		fmt.Fprintf(&text, "(%d earlier turns elided)\n", elided)
	}
	for _, e := range entries {
		fmt.Fprintf(&text, "%s: %s\n", e.Role, e.Text)
	}
	return text.String()
}
