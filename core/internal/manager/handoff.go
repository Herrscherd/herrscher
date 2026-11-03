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
	text.WriteString("[Reprise de conversation après changement de modèle. Contexte du fil précédent ci-dessous ; poursuis-le.]\n")
	if elided > 0 {
		fmt.Fprintf(&text, "(%d tours antérieurs élidés)\n", elided)
	}
	for _, e := range entries {
		fmt.Fprintf(&text, "%s: %s\n", e.Role, e.Text)
	}
	return text.String()
}
