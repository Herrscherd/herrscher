package host

import (
	"log/slog"
	"strings"
	"sync"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type modelTerms struct {
	vendor string
	model  string
	window int
	rates  modelRates
}

func modelTermsFor(models *ModelDiscovery) func(state.Session) modelTerms {
	resolve := resolvedTermsFor(models)
	return func(session state.Session) modelTerms {
		terms, err := resolve(session)
		if err != nil {
			return modelTerms{vendor: session.Vendor, model: sessionModel(session)}
		}
		return terms
	}
}

func resolvedTermsFor(models *ModelDiscovery) func(state.Session) (modelTerms, error) {
	sources := models.Sources()
	policy := ResolvePolicy(envx.Getenv)
	plugins := contracts.Default.Backends()
	return func(session state.Session) (modelTerms, error) {
		modelID := sessionModel(session)
		terms := modelTerms{vendor: session.Vendor, model: modelID}
		if modelID == "" {
			return terms, nil
		}
		entry, err := lookupModel(plugins, policy, sources, modelID)
		if err != nil {
			return terms, err
		}
		terms.window = entry.ContextWindow
		terms.rates = ratesOf(entry.ModelSpec)
		return terms, nil
	}
}

type unresolvedWarnings struct {
	session string
	mu      sync.Mutex
	warned  map[string]bool
}

func (w *unresolvedWarnings) warnOnce(modelID string, err error) {
	w.mu.Lock()
	if w.warned[modelID] {
		w.mu.Unlock()
		return
	}
	w.warned[modelID] = true
	w.mu.Unlock()
	slog.Warn("session model no longer resolves, its context window and cost estimate fall back to the defaults",
		"session", w.session, "model", modelID, "err", err)
}

func liveModelTerms(store *state.State, models *ModelDiscovery, name string) func() modelTerms {
	resolve := resolvedTermsFor(models)
	warnings := &unresolvedWarnings{session: name, warned: make(map[string]bool)}
	return func() modelTerms {
		session, ok := store.FindSession(name)
		if !ok {
			return modelTerms{}
		}
		terms, err := resolve(session)
		if err != nil {
			warnings.warnOnce(sessionModel(session), err)
		}
		return terms
	}
}

func sessionModel(session state.Session) string {
	if session.ModelID != "" {
		return session.ModelID
	}
	return invocationFlag(session.Cmd, "--model")
}

func sessionEffort(s state.Session) string {
	return invocationFlag(s.Cmd, "--effort")
}

func invocationFlag(cmd, flag string) string {
	fields := strings.Fields(cmd)
	for index, field := range fields {
		if field == flag && index+1 < len(fields) {
			return fields[index+1]
		}
		if v, ok := strings.CutPrefix(field, flag+"="); ok {
			return v
		}
	}
	return ""
}
