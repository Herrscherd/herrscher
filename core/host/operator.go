package host

import (
	"context"
	"fmt"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

var forwardedLocally = map[string]bool{"seed": true}

var daemonOnly = map[string]bool{"approve ask": true}

var daemonOwned = [][]string{{"session"}, {"host"}, {"approve"}, {"role"}}

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
