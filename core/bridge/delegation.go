package bridge

import (
	"fmt"
	"maps"
	"slices"
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

const delegationRoleIntro = "Some agents below list worker roles. A role runs its worker on a model set " +
	"for that role, which may be a smaller and cheaper one: name it after the agent with a slash, " +
	"as in \"⟢ delegate: <agent>/<role> \u2014 <mission>\". Use only the roles listed for that agent."

type workerRoster interface {
	WorkerModels(agent string) map[string]string
}

func delegationBlock(roster contracts.RosterProvider) string {
	if roster == nil {
		return ""
	}
	agents := roster.Agents()
	if len(agents) == 0 {
		return ""
	}
	roles := workerRolesOf(roster, agents)
	var out strings.Builder
	out.WriteString("<delegation>\n")
	out.WriteString(delegationIntro)
	if len(roles) > 0 {
		out.WriteString("\n")
		out.WriteString(delegationRoleIntro)
	}
	out.WriteString("\nAvailable agents:\n")
	for _, a := range agents {
		out.WriteString(delegationLine(a, roles[a.Name]))
	}
	out.WriteString("</delegation>")
	return out.String()
}

func workerRolesOf(roster contracts.RosterProvider, agents []contracts.AgentInfo) map[string]map[string]string {
	workers, ok := roster.(workerRoster)
	if !ok {
		return nil
	}
	var roles map[string]map[string]string
	for _, info := range agents {
		models := workers.WorkerModels(info.Name)
		if len(models) == 0 {
			continue
		}
		if roles == nil {
			roles = make(map[string]map[string]string)
		}
		roles[info.Name] = models
	}
	return roles
}

func delegationLine(agent contracts.AgentInfo, roles map[string]string) string {
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
	if len(roles) > 0 {
		line += " roles: " + workerRoleList(roles)
	}
	return line + "\n"
}

func workerRoleList(roles map[string]string) string {
	names := slices.Sorted(maps.Keys(roles))
	pairs := make([]string, 0, len(names))
	for _, name := range names {
		pairs = append(pairs, name+"="+roles[name])
	}
	return strings.Join(pairs, ", ")
}
