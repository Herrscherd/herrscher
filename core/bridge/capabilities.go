package bridge

import "strings"

// capabilitiesIntro tells the model what it is running inside and how to reach
// it. An agent here has a shell, and `herrscher` is on it — but nothing in a
// turn's prompt ever said so, so an agent asked to read a chat channel would
// look for a token of its own and conclude the platform was unreachable, while
// the daemon supervising it held that gateway open. The block is the smallest
// thing that closes the gap: what runs it, how to talk to it, and where the full
// usage lives.
const capabilitiesIntro = "You are one session of a running Herrscher daemon: it supervises this session and holds " +
	"open the gateways (chat platforms) this deployment is connected to. " +
	"That daemon is reachable from your shell as `herrscher <verb>` — the same binary, already running — " +
	"so a verb below acts on the live system, this session included, using the daemon's own credentials. " +
	"Do not go around it with a token or an API of your own. " +
	"`herrscher commands` prints every verb with its parameters. " +
	"A verb that ends a session or discards work is the operator's call to make, not yours."

// withCapabilities appends a <capabilities> block — the intro plus the daemon's
// verbs — to baseCtx, mirroring withSkills and withDelegation. An empty summary
// (no daemon handed one down) returns baseCtx unchanged, so a bridge run without
// one is exactly as before.
func withCapabilities(baseCtx, summary string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return baseCtx
	}
	var b strings.Builder
	if baseCtx != "" {
		b.WriteString(baseCtx)
		b.WriteString("\n\n")
	}
	b.WriteString("<capabilities>\n")
	b.WriteString(capabilitiesIntro)
	b.WriteString("\nVerbs this daemon dispatches:\n")
	b.WriteString(summary)
	b.WriteString("\n</capabilities>")
	return b.String()
}
