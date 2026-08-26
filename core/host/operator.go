package host

import (
	"context"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

// forwardedLocally names the verbs of a daemon-owned family that this process
// answers itself. `session seed` is one: the daemon resolves it itself, carrying
// the turn identity it already settled on, and forwarding it twice would hand
// the daemon a different turn.
//
// `approve hook` needs no entry. It must run in the process the vendor spawned,
// because the payload it answers is on THAT process's stdin, and it is not a
// registered verb at all: the binary answers it before any registry exists, so
// there is nothing here for an interceptor to catch.
var forwardedLocally = map[string]bool{"seed": true}

// daemonOnly names the verbs only a running daemon can answer, keyed on the
// full command path so a future verb that happens to share a last word is not
// caught by accident.
//
// `approve ask` is one: the request it opens waits for a human, and the humans
// reach it through the daemon. Run here, in a process that exits with the
// command, there is nobody to ask, so it would register a request no one can
// see and wait out the whole approval timeout before denying. Failing at once,
// naming what is missing, is the honest answer.
var daemonOnly = map[string]bool{"approve ask": true}

// daemonOwned are the verb families whose rows live in state.json, which the
// daemon holds in memory and rewrites whole on every turn.
var daemonOwned = [][]string{{"session"}, {"host"}, {"approve"}}

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
// The daemonOnly verbs are the exception: running them here would answer wrong,
// so they say the daemon is missing instead.
func forwardDaemonOwnedCommands(reg *cli.Registry, instID string, forward seedCommandForwarder) {
	for _, prefix := range daemonOwned {
		reg.Intercept(prefix, func(cmd contracts.Cmd, next cli.Runner) cli.Runner {
			if len(cmd.Path) > 1 && forwardedLocally[cmd.Path[1]] {
				return next
			}
			return func(ctx context.Context, in contracts.Input) (string, error) {
				target := commandSocketTarget(instID)
				out, handled, err := forward(ctx, target, argvOf(cmd, in))
				if handled {
					return out, err
				}
				if path := strings.Join(cmd.Path, " "); daemonOnly[path] {
					return "", fmt.Errorf("`%s` needs a running herrscher daemon, and none answered on %s", path, target)
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
