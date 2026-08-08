package host

import (
	"sort"
	"strings"

	"github.com/Herrscherd/herrscher/core/cli"
)

// EnvCapabilities carries the daemon's verb summary down to the bridge child,
// which turns it into the <capabilities> block every turn's prompt gets. It is
// handed over the environment, like the gateway pair, because only the daemon
// has the complete registry: a gateway's contributed verbs (`<gateway> channel
// read`, …) exist only where that gateway is instantiated, so a summary built
// anywhere else would be a shorter list wearing the same name.
const EnvCapabilities = "HERRSCHER_CAPABILITIES"

// CapabilityEnvPair renders the KEY=VALUE entry for a registry's summary, or ""
// when there is nothing to say.
func CapabilityEnvPair(specs []cli.Spec) string {
	s := CapabilitySummary(specs)
	if s == "" {
		return ""
	}
	return EnvCapabilities + "=" + s
}

// CapabilitySummary renders what the daemon dispatches, one line per family:
//
//	session — archive, close, create, info, …
//	gateway — channel post, channel read
//
// Families rather than a full usage block because this is injected into every
// turn: the point is that the agent knows the verb exists and who answers it,
// and `herrscher commands` (named in the block's intro) prints the parameters
// on demand. Sorted, so two daemons with the same build produce the same text
// and the prompt cache is not invalidated by map order.
func CapabilitySummary(specs []cli.Spec) string {
	families := map[string][]string{}
	var order []string
	for _, s := range specs {
		if len(s.Path) == 0 {
			continue
		}
		head := s.Path[0]
		if _, seen := families[head]; !seen {
			order = append(order, head)
			families[head] = nil
		}
		// A one-word command (`commands`) has no tail to list under its own name,
		// and a " — " with nothing after it reads as a truncation.
		if tail := strings.Join(s.Path[1:], " "); tail != "" {
			families[head] = append(families[head], tail)
		}
	}
	sort.Strings(order)
	var b strings.Builder
	for _, head := range order {
		verbs := families[head]
		sort.Strings(verbs)
		b.WriteString("  ")
		b.WriteString(head)
		if len(verbs) > 0 {
			b.WriteString(" — ")
			b.WriteString(strings.Join(verbs, ", "))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}
