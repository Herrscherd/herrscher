package host

import (
	"context"
	"fmt"
	"os"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

// BuildFirstMemory construit le premier plugin mémoire enregistré depuis sa
// config résolue. Utilisé par les verbes CLI `memory`. Le caller ferme la
// mémoire. (Le plugin-host garde sa propre variante firstMemory qui renvoie
// aussi le manifest pour l'announce.)
func BuildFirstMemory(ctx context.Context) (contracts.Memory, error) {
	for _, p := range contracts.Default.Memories() {
		if p.Memory == nil {
			continue
		}
		cfg, err := contracts.Resolve(p.Manifest.Config, os.Getenv)
		if err != nil {
			return nil, fmt.Errorf("memory: %w", err)
		}
		return p.Memory(ctx, cfg)
	}
	return nil, fmt.Errorf("memory: no memory plugin registered")
}
