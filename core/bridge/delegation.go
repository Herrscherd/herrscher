package bridge

import (
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// delegationIntro is the always-on affordance: it tells the model the async
// hand-off exists, how to trigger it, and its one hard precondition. The agent
// roster is appended per session.
const delegationIntro = "You can hand a full mission to another agent — it runs autonomously in its own " +
	"isolated worktree on its own backend and reports back to you when it is done " +
	"(async: you keep talking to the human meanwhile). To trigger it, end your reply " +
	"with ONE trailer line:\n" +
	"  ⟢ delegate: <agent> — <mission>   spawn a named worker and get its result back\n" +
	"  ⟢ route: <mission>                let the host pick the best-matching agent\n" +
	"When a worker's result later lands in your turn, synthesize it for the human. " +
	"This requires your session to be an isolated, committed worktree."

// withDelegation appends a <delegation> block — the intro plus the available
// agents — to baseCtx, mirroring withSkills. A nil roster or one that lists no
// agents returns baseCtx unchanged, so a deployment with no delegates is exactly
// as before.
func withDelegation(baseCtx string, roster contracts.RosterProvider) string {
	if roster == nil {
		return baseCtx
	}
	agents := roster.Agents()
	if len(agents) == 0 {
		return baseCtx
	}
	var b strings.Builder
	if baseCtx != "" {
		b.WriteString(baseCtx)
		b.WriteString("\n\n")
	}
	b.WriteString("<delegation>\n")
	b.WriteString(delegationIntro)
	b.WriteString("\nAvailable agents:\n")
	for _, a := range agents {
		b.WriteString(delegationLine(a))
	}
	b.WriteString("</delegation>")
	return b.String()
}

// delegationLine renders one roster entry: "  - name (backend: X) — summary [tags]".
func delegationLine(a contracts.AgentInfo) string {
	backend := a.Backend
	if backend == "" {
		backend = "host default"
	}
	line := fmt.Sprintf("  - %s (backend: %s)", a.Name, backend)
	if a.Summary != "" {
		line += " — " + a.Summary
	}
	if len(a.Tags) > 0 {
		line += " [" + strings.Join(a.Tags, " ") + "]"
	}
	return line + "\n"
}
