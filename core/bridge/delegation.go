package bridge

import (
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

const delegationIntro = "You can hand a full mission to another agent. It runs autonomously in its own " +
	"isolated worktree on its own backend and reports back to you when it is done " +
	"(async: you keep talking to the human meanwhile). To trigger it, end your reply " +
	"with ONE trailer line:\n" +
	"  ⟢ delegate: <agent> \u2014 <mission>   spawn a named worker and get its result back\n" +
	"  ⟢ route: <mission>                let the host pick the best-matching agent\n" +
	"When a worker's result later lands in your turn, synthesize it for the human. " +
	"This requires your session to be an isolated, committed worktree."

func withDelegation(baseCtx string, roster contracts.RosterProvider) string {
	if roster == nil {
		return baseCtx
	}
	agents := roster.Agents()
	if len(agents) == 0 {
		return baseCtx
	}
	var out strings.Builder
	if baseCtx != "" {
		out.WriteString(baseCtx)
		out.WriteString("\n\n")
	}
	out.WriteString("<delegation>\n")
	out.WriteString(delegationIntro)
	out.WriteString("\nAvailable agents:\n")
	for _, a := range agents {
		out.WriteString(delegationLine(a))
	}
	out.WriteString("</delegation>")
	return out.String()
}

func delegationLine(agent contracts.AgentInfo) string {
	backend := agent.Backend
	if backend == "" {
		backend = "host default"
	}
	line := fmt.Sprintf("  - %s (backend: %s)", agent.Name, backend)
	if agent.Summary != "" {
		line += ": " + agent.Summary
	}
	if len(agent.Tags) > 0 {
		line += " [" + strings.Join(agent.Tags, " ") + "]"
	}
	return line + "\n"
}
