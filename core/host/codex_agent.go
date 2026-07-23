package host

import "github.com/Herrscherd/herrscher/core/internal/agent"

// codexDefaultTags are the capability tags the auto-provisioned Codex delegate
// advertises — the kinds of mission it is a sensible default for and what
// ⟢ route: matches against.
var codexDefaultTags = []string{"refactor", "tests", "mechanical"}

// ensureCodexAgent makes the default Codex delegate exist so cross-model
// delegation works out of the box. It is idempotent and never overwrites an
// existing "codex" agent (a manual `agent create` or a prior boot wins), and it
// is best-effort: a creation failure is swallowed so a boot never blocks on it.
func ensureCodexAgent(store *agent.Store) {
	if _, ok := store.Get("codex"); ok {
		return
	}
	_, _ = store.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: codexDefaultTags})
}
