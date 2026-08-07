package host

import (
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// CollectCommands collects the verbs the loaded gateways contribute, each
// namespaced under the contributing plugin's own Kind:
//
//	<kindA> channel read
//	<kindB> channel read
//
// The prefix is imposed by the host and never chosen by the plugin. That is what
// makes a cross-plugin collision impossible rather than merely unlikely: two
// plugins have two distinct Kinds, and if they did not, plugin loading would
// have failed long before their commands did. It also keeps this file agnostic —
// the Kind is a string read out of a manifest, never one written here.
//
// A duplicate path within a single plugin is still possible, and it is refused
// by name: a verb that quietly went missing would send the operator debugging
// the command instead of the build.
//
// Scope: only gateway sets are scanned. A plugin of any other category that
// implements CommandSource contributes nothing, and does so silently — the
// daemon owns gateways as live objects and has no equivalent handle on the
// rest. Widening that is a deliberate change, not an oversight to fix in place.
//
// It must be called on gateways the composition root has not yet wrapped: a
// decorator's method set is fixed and drops Commands, so a wrapped gateway
// never satisfies CommandSource and every verb it offers is lost in silence.
func CollectCommands(gws []contracts.GatewaySet) ([]contracts.Cmd, error) {
	var out []contracts.Cmd
	for _, g := range gws {
		if g.Gateway == nil {
			continue
		}
		src, ok := g.Gateway.(contracts.CommandSource)
		if !ok {
			continue
		}
		kind := g.Gateway.Manifest().Kind
		seen := map[string]bool{}
		for _, c := range src.Commands() {
			path := strings.Join(c.Path, " ")
			if seen[path] {
				return nil, fmt.Errorf("plugin %q contributes %q twice", kind, path)
			}
			seen[path] = true
			c.Path = append([]string{kind}, c.Path...)
			out = append(out, c)
		}
	}
	return out, nil
}

// contributedKinds reduces collected commands to the namespaces they occupy —
// the set the daemon consults to tell a plugin verb from one of its own. The
// first path element is enough: CollectCommands writes the kind there itself,
// and a kind colliding with a built-in never survives registry construction.
func contributedKinds(cmds []contracts.Cmd) map[string]bool {
	kinds := map[string]bool{}
	for _, c := range cmds {
		if len(c.Path) > 0 {
			kinds[c.Path[0]] = true
		}
	}
	return kinds
}
