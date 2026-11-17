package main

import (
	"context"
	"os"

	"github.com/Herrscherd/herrscher/core/host"
)

const hookBlockingExit = 2

func runApprove(ctx context.Context, layout host.RuntimeLayout, args []string) error {
	if len(args) > 0 && args[0] == "hook" {
		if err := host.RunApprovalHook(ctx, layout, os.Stdin, os.Stdout, os.Stderr); err != nil {
			return exitStatusError{code: hookBlockingExit}
		}
		return nil
	}
	return runRegistryVerb(ctx, layout, "approve", args)
}

func runSession(ctx context.Context, layout host.RuntimeLayout, args []string) error {
	return runRegistryVerb(ctx, layout, verbSession, args)
}

func runAgent(ctx context.Context, layout host.RuntimeLayout, args []string) error {
	return runRegistryVerb(ctx, layout, "agent", args)
}

func runHost(ctx context.Context, layout host.RuntimeLayout, args []string) error {
	return runRegistryVerb(ctx, layout, "host", args)
}

func runRole(ctx context.Context, layout host.RuntimeLayout, args []string) error {
	return runRegistryVerb(ctx, layout, "role", args)
}

func runMemory(ctx context.Context, layout host.RuntimeLayout, args []string) error {
	return runRegistryVerb(ctx, layout, "memory", args)
}

func runModels(ctx context.Context, layout host.RuntimeLayout, args []string) error {
	return runRegistryVerb(ctx, layout, "models", args)
}
