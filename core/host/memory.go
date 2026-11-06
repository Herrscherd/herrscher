package host

import (
	"context"
	"os"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// BuildFirstMemoryWithManifest builds the first registered memory plugin from
// its resolved config, returning it with its manifest for announcement. It is
// the single source both the CLI verbs (via BuildFirstMemory) and the
// plugin-host (via firstMemory) build memory through.
func BuildFirstMemoryWithManifest(ctx context.Context) (contracts.Memory, contracts.Manifest, error) {
	memories := contracts.Default.Memories()
	for idx := range memories {
		plugin := memories[idx]
		if plugin.Memory == nil {
			continue
		}
		cfg, err := contracts.Resolve(plugin.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, contracts.Manifest{}, wrapHost(err, "memory.wrap", nil)
		}
		mem, err := plugin.Memory(ctx, cfg)
		if err != nil {
			return nil, contracts.Manifest{}, wrapHost(err, "memory.wrap", nil)
		}
		return mem, plugin.Manifest, nil
	}
	return nil, contracts.Manifest{}, errNoMemoryPlugin
}

// BuildFirstMemory builds the first registered memory plugin without its
// manifest, the form the CLI verbs need. The caller closes the memory.
func BuildFirstMemory(ctx context.Context) (contracts.Memory, error) {
	m, _, err := BuildFirstMemoryWithManifest(ctx)
	return m, err
}
