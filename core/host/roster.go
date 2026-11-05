package host

import (
	"path/filepath"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

type storeRoster struct{ store *agent.Store }

// DefaultAgentsRoot returns the directory holding agent homes, derived the same
// way the daemon derives it (the "agents" dir beside the state file), so the
// separate bridge process resolves the identical roster the coordinator sees.
func DefaultAgentsRoot() string {
	return filepath.Join(filepath.Dir(DefaultStatePath()), "agents")
}

// NewRoster builds a RosterProvider over the agent homes under root.
func NewRoster(root string) contracts.RosterProvider {
	return storeRoster{store: agent.NewStore(root)}
}

func (r storeRoster) Agents() []contracts.AgentInfo {
	list, err := r.store.List()
	if err != nil {
		return nil
	}
	out := make([]contracts.AgentInfo, 0, len(list))
	for i := range list {
		a := &list[i]
		out = append(out, contracts.AgentInfo{Name: a.Name, Backend: a.Backend, Tags: a.Tags})
	}
	return out
}
