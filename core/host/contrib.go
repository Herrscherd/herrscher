package host

import (
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
// have failed long before their commands did. It also keeps this file agnostic, since
// the Kind is a string read out of a manifest, never one written here.
//
// A duplicate path within a single plugin is still possible, and it is refused
// by name: a verb that quietly went missing would send the operator debugging
// the command instead of the build.
//
// Scope: only gateway sets are scanned. A plugin of any other category that
// implements CommandSource contributes nothing, and does so silently: the
// daemon owns gateways as live objects and has no equivalent handle on the
// rest. Widening that is a deliberate change, not an oversight to fix in place.
//
// It must be called on gateways the composition root has not yet wrapped: a
// decorator's method set is fixed and drops Commands, so a wrapped gateway
// never satisfies CommandSource and every verb it offers is lost in silence.
func CollectCommands(gws []contracts.GatewaySet) ([]contracts.Cmd, error) {
	var out []contracts.Cmd
	for _, gateway := range gws {
		if gateway.Gateway == nil {
			continue
		}
		src, ok := gateway.Gateway.(contracts.CommandSource)
		if !ok {
			continue
		}
		kind := gateway.Gateway.Manifest().Kind
		seen := make(map[string]bool)
		for _, command := range src.Commands() {
			path := strings.Join(command.Path, " ")
			if seen[path] {
				return nil, &contributionClashError{Kind: kind, Path: path}
			}
			seen[path] = true
			command.Path = append([]string{kind}, command.Path...)
			out = append(out, command)
		}
	}
	return out, nil
}

func contributedKinds(cmds []contracts.Cmd) map[string]bool {
	kinds := make(map[string]bool)
	for _, command := range cmds {
		if len(command.Path) > 0 {
			kinds[command.Path[0]] = true
		}
	}
	return kinds
}
