package manager

import (
	"fmt"
	"strings"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

// handoffSummaryTail is how many trailing turns the "summary" handoff keeps
// verbatim; older turns are elided with a count note. A deterministic condensation,
// not an LLM summary — cheaper than replaying everything, with the recent context intact.
const handoffSummaryTail = 12

// buildHandoffSeed formats a prior transcript into the opening turn for a
// just-switched backend. `full` replays every turn; `summary` keeps the last
// handoffSummaryTail turns and notes how many were dropped. Empty transcript → "".
func buildHandoffSeed(entries []state.TranscriptEntry, mode string) string {
	if len(entries) == 0 {
		return ""
	}
	elided := 0
	if mode == "summary" && len(entries) > handoffSummaryTail {
		elided = len(entries) - handoffSummaryTail
		entries = entries[elided:]
	}
	var b strings.Builder
	b.WriteString("[Reprise de conversation après changement de modèle. Contexte du fil précédent ci-dessous ; poursuis-le.]\n")
	if elided > 0 {
		fmt.Fprintf(&b, "(%d tours antérieurs élidés)\n", elided)
	}
	for _, e := range entries {
		fmt.Fprintf(&b, "%s: %s\n", e.Role, e.Text)
	}
	return b.String()
}
