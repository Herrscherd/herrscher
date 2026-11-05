package bridge

import "strings"

const capabilitiesIntro = "You are one session of a running Herrscher daemon: it supervises this session and holds " +
	"open the gateways (chat platforms) this deployment is connected to. " +
	"That daemon is reachable from your shell as `herrscher <verb>`, the same binary, already running, " +
	"so a verb below acts on the live system, this session included, using the daemon's own credentials. " +
	"Do not go around it with a token or an API of your own. " +
	"`herrscher commands` prints every verb the daemon dispatches, with its parameters; the list below is the part " +
	"you may run, and the daemon refuses the rest to a session. " +
	"A verb that ends a session, rewrites the approval policy, or acts on a session other than yours is the " +
	"operator's to run, and asking them is the way to it. " +
	"A handoff (`session handoff`) opens a new session with a brief of this one: run it only when the operator asks for it. Its limit per session tree is a local guard that resets when the daemon restarts, and a handoff whose brief started counts toward it even if it fails. When the operator closes a session, its direct children become roots and their quota starts again."

func withCapabilities(baseCtx, summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return baseCtx
	}
	var out strings.Builder
	if baseCtx != "" {
		out.WriteString(baseCtx)
		out.WriteString("\n\n")
	}
	out.WriteString("<capabilities>\n")
	out.WriteString(capabilitiesIntro)
	out.WriteString("\nVerbs you may run:\n")
	out.WriteString(summary)
	out.WriteString("\n</capabilities>")
	return out.String()
}
