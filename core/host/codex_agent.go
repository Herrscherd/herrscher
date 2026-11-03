package host

import "github.com/Herrscherd/herrscher/core/internal/agent"

func codexDefaultTags() []string {
	return []string{"refactor", "tests", "mechanical"}
}

func ensureCodexAgent(store *agent.Store) {
	if _, ok := store.Get("codex"); ok {
		return
	}
	_, createErr := store.Create(agent.CreateSpec{Name: "codex", Backend: "codex", Tags: codexDefaultTags()})
	logCleanup(createErr)
}
