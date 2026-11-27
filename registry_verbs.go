package main

import (
	"context"
	"os"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
)

const hookBlockingExit = 2

func runApprove(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	if len(args) > 0 && args[0] == "hook" {
		if err := host.RunApprovalHook(ctx, layout, hostMessages, os.Stdin, os.Stdout, os.Stderr, host.ParseHookOptions(args[1:])); err != nil {
			return exitStatusError{code: hookBlockingExit}
		}
		return nil
	}
	return runRegistryVerb(ctx, layout, messages, hostMessages, "approve", args)
}

func runSession(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	if len(args) > 0 && args[0] == watchVerb {
		return runSessionWatch(ctx, layout, messages, hostMessages, args[1:])
	}
	return runRegistryVerb(ctx, layout, messages, hostMessages, verbSession, args)
}

func runAgent(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	return runRegistryVerb(ctx, layout, messages, hostMessages, "agent", args)
}

func runHost(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	return runRegistryVerb(ctx, layout, messages, hostMessages, "host", args)
}

func runRole(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	return runRegistryVerb(ctx, layout, messages, hostMessages, "role", args)
}

func runMCPWeight(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	ctx, cancel := context.WithTimeout(ctx, host.MCPWeightDeadline)
	defer cancel()
	return runRegistryVerb(ctx, layout, messages, hostMessages, "mcp", args)
}

func runMemory(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	return runRegistryVerb(ctx, layout, messages, hostMessages, "memory", args)
}

func runModels(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	return runRegistryVerb(ctx, layout, messages, hostMessages, "models", args)
}
