package manager

import (
	"regexp"
	"strings"
)

const maxGateways = 16

var gatewayKindRe = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ParseGateways turns the `--gateways a,b` flag (and the `--terminal_only`
// shorthand) into the ordered, de-duplicated gateway set stored on a session.
// Entries are lowercased and must match [a-z0-9_-]; invalid entries are dropped.
// An explicit list always wins; an empty list with terminalOnly yields
// ["terminal"]; an empty list otherwise falls back to defaults — the primary
// gateway kinds the composition root injects (it names the concrete platform,
// the core never does). A nil/empty defaults yields the same, i.e. nothing.
func ParseGateways(list string, terminalOnly bool, defaults []string) []string {
	// Common path: no explicit list. Skip the split/dedup machinery and return
	// the default straight away.
	if strings.TrimSpace(list) == "" {
		return gatewayDefault(terminalOnly, defaults)
	}
	out := make([]string, 0, maxGateways)
	seen := map[string]bool{}
	for _, p := range strings.Split(list, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || seen[p] || !gatewayKindRe.MatchString(p) {
			continue
		}
		seen[p] = true
		out = append(out, p)
		if len(out) == maxGateways {
			break
		}
	}
	if len(out) > 0 {
		return out
	}
	return gatewayDefault(terminalOnly, defaults)
}

// gatewayDefault picks the binding for an empty/invalid gateway list: the
// terminal gateway for a terminal-only session, else the injected primary set.
func gatewayDefault(terminalOnly bool, defaults []string) []string {
	if terminalOnly {
		return []string{"terminal"}
	}
	return append([]string(nil), defaults...)
}
