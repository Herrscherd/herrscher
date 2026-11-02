package host

import "github.com/Herrscherd/herrscher/core/internal/agent"

var codexDefaultTags = []string{"refactor", "tests", "mechanical"}

func ensureCodexAgent(store *agent.Store) {
	if _, ok := store.Get("codex"); ok {
		return
	}
	_, _ = store.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: codexDefaultTags})
}
