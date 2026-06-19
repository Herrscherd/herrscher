package host

import (
	"context"
	"fmt"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// Resolver turns registered plugins into live port objects, choosing local
// (in-proc factory) or remote (gRPC proxy) per category. With a nil/empty
// remote set every category resolves local — today's behaviour.
type Resolver struct {
	remote  map[contracts.Category]bool
	NatsURL string // "" => nats.DefaultURL; consulted only on the remote path (Task 8)
}

func NewResolver(remote map[contracts.Category]bool, natsURL string) *Resolver {
	return &Resolver{remote: remote, NatsURL: natsURL}
}

func (r *Resolver) isRemote(c contracts.Category) bool {
	return r.remote != nil && r.remote[c]
}

// Memory resolves the first registered memory plugin. Local: call the factory.
// Remote: dialed in a later task. Returns nil (no error) when none is registered
// — memory stays optional, matching buildMemory's contract.
func (r *Resolver) Memory(ctx context.Context, plugins []contracts.Plugin, getenv func(string) string) (contracts.Memory, error) {
	for _, p := range plugins {
		if p.Memory == nil {
			continue
		}
		if r.isRemote(contracts.CategoryMemory) {
			return r.dialRemoteMemory(ctx, p)
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, getenv)
		if err != nil {
			return nil, err
		}
		return p.Memory(ctx, cfg)
	}
	return nil, nil
}

// dialRemoteMemory is completed in the next task.
func (r *Resolver) dialRemoteMemory(_ context.Context, p contracts.Plugin) (contracts.Memory, error) {
	return nil, fmt.Errorf("resolver: remote memory %q not wired yet", p.Manifest.Kind)
}
