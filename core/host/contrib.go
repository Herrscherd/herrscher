package host

import (
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// contributedCommands collects the verbs the loaded gateways contribute, each
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
func contributedCommands(gws []contracts.GatewaySet) ([]contracts.Cmd, error) {
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
