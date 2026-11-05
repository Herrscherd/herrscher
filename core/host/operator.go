package host

import (
	"context"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
)

func forwardedLocally() map[string]bool {
	return map[string]bool{verbSeed: true}
}

func daemonOnly() map[string]bool {
	return map[string]bool{"approve ask": true}
}

func daemonOwned() [][]string {
	return [][]string{{nsSession}, {"host"}, {nsApprove}, {"role"}}
}

func forwardDaemonOwnedCommands(reg *cli.Registry, instID string, forward seedCommandForwarder) {
	for _, prefix := range daemonOwned() {
		reg.Intercept(prefix, func(cmd contracts.Cmd, next cli.Runner) cli.Runner {
			if len(cmd.Path) > 1 && forwardedLocally()[cmd.Path[1]] {
				return next
			}
			return func(ctx context.Context, input contracts.Input) (contracts.Result, error) {
				target := commandSocketTarget(instID)
				out, handled, err := forward(ctx, target, argvOf(cmd, input))
				if handled {
					return contracts.DataResult(out), err
				}
				if path := strings.Join(cmd.Path, " "); daemonOnly()[path] {
					return contracts.Result{}, &daemonRequiredError{Command: path, Target: target}
				}
				return next(ctx, input)
			}
		})
	}
}

func argvOf(cmd contracts.Cmd, input contracts.Input) []string {
	argv := append([]string(nil), cmd.Path...)
	for _, param := range cmd.Params {
		value, ok := input.Args[param.Name]
		if !ok {
			continue
		}
		if !param.ValueRequired && value == valueTrue {
			argv = append(argv, "--"+param.Name)
			continue
		}
		argv = append(argv, "--"+param.Name, value)
	}
	argv = append(argv, input.Rest...)
	if input.JSON {
		argv = append(argv, "--json")
	}
	return argv
}
