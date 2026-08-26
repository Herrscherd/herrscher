package host

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

// The daemon resolves this one itself, carrying the turn identity it already
// settled on; forwarding it twice would hand the daemon a different turn.
var forwardedLocally = map[string]bool{"seed": true}

// daemonOwned are the verb families whose rows live in state.json, which the
// daemon holds in memory and rewrites whole on every turn.
var daemonOwned = [][]string{{"session"}, {"host"}}

// forwardDaemonOwnedCommands sends those verbs to the running daemon instead of
// running them here.
//
// The operator CLI and the daemon are two processes over one state file, each
// holding its own copy in memory. A session closed from the CLI left the daemon
// unaware: it kept offering the session in autocomplete, and its next save wrote
// the session back. A host added from the CLI is the same story told forwards:
// the daemon's next save would drop the record, and the session that named it
// would refuse for want of a host the operator had just registered. The daemon
// owns those rows, so it is the one that must decide; this process only asks.
//
// With no daemon listening the dial misses, nothing is handled, and the command
// runs locally exactly as before — a single-shot install has no daemon to ask.
func forwardDaemonOwnedCommands(reg *cli.Registry, instID string, forward seedCommandForwarder) {
	for _, prefix := range daemonOwned {
		reg.Intercept(prefix, func(cmd contracts.Cmd, next cli.Runner) cli.Runner {
			if len(cmd.Path) > 1 && forwardedLocally[cmd.Path[1]] {
				return next
			}
			return func(ctx context.Context, in contracts.Input) (string, error) {
				if out, handled, err := forward(ctx, commandSocketTarget(instID), argvOf(cmd, in)); handled {
					return out, err
				}
				return next(ctx, in)
			}
		})
	}
}

// argvOf renders a parsed invocation back to the argv the daemon parses again on
// the other side of the socket. A valueless boolean goes back valueless: passing
// "--force true" would leave "true" as a stray positional.
func argvOf(cmd contracts.Cmd, in contracts.Input) []string {
	argv := append([]string(nil), cmd.Path...)
	for _, p := range cmd.Params {
		v, ok := in.Args[p.Name]
		if !ok {
			continue
		}
		if !p.ValueRequired && v == "true" {
			argv = append(argv, "--"+p.Name)
			continue
		}
		argv = append(argv, "--"+p.Name, v)
	}
	argv = append(argv, in.Rest...)
	if in.JSON {
		argv = append(argv, "--json")
	}
	return argv
}
