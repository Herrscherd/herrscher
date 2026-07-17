package host

import (
	"context"
	"fmt"
	"os"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// BuildFirstMemoryWithManifest builds the first registered memory plugin from
// its resolved config, returning it with its manifest for announcement. It is
// the single source both the CLI verbs (via BuildFirstMemory) and the
// plugin-host (via firstMemory) build memory through.
func BuildFirstMemoryWithManifest(ctx context.Context) (contracts.Memory, contracts.Manifest, error) {
	for _, p := range contracts.Default.Memories() {
		if p.Memory == nil {
			continue
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, contracts.Manifest{}, fmt.Errorf("memory: %w", err)
		}
		m, err := p.Memory(ctx, cfg)
		if err != nil {
			return nil, contracts.Manifest{}, fmt.Errorf("memory: %w", err)
		}
		return m, p.Manifest, nil
	}
	return nil, contracts.Manifest{}, fmt.Errorf("memory: no memory plugin registered")
}

// BuildFirstMemory builds the first registered memory plugin without its
// manifest — the form the CLI verbs need. The caller closes the memory.
func BuildFirstMemory(ctx context.Context) (contracts.Memory, error) {
	m, _, err := BuildFirstMemoryWithManifest(ctx)
	return m, err
}
