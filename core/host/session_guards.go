package host

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	selfRestartWindow  = 10 * time.Minute
	verbServiceRestart = "service restart"
	verbEnvSet         = "env set"
	verbSetBudget      = "session set-budget"
)

type selfRestarts struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func newSelfRestarts() *selfRestarts {
	return &selfRestarts{last: make(map[string]time.Time), now: time.Now}
}

func (r *selfRestarts) admit(session, verb string) error {
	if r == nil || verb != verbServiceRestart {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if previous, ok := r.last[session]; ok && now.Sub(previous) < selfRestartWindow {
		return &accessDeniedError{Reason: fmt.Sprintf(
			"herrscher: session %q already restarted the daemon %s ago; a session restarts it once every %s.",
			session, now.Sub(previous).Round(time.Second), selfRestartWindow)}
	}
	r.last[session] = now
	return nil
}

func (h *hub) guardSessionCall(session, verb string, input contracts.Input) error {
	switch verb {
	case verbEnvSet:
		if isSecretEnvKey(input.Get(paramKey)) {
			return &accessDeniedError{Reason: errSecretSession.Error()}
		}
	case verbSetBudget:
		return budgetWithinParent(h.st, session, input)
	}
	return nil
}

type budgetCap struct {
	param  string
	parent func(state.Session) float64
}

func budgetCaps() []budgetCap {
	return []budgetCap{
		{"cost_cap", func(s state.Session) float64 { return s.CostCap }},
		{"token_cap", func(s state.Session) float64 { return float64(s.TokenCap) }},
		{"cohort_cost_cap", func(s state.Session) float64 { return s.CohortCostCap }},
		{"cohort_token_cap", func(s state.Session) float64 { return float64(s.CohortTokenCap) }},
	}
}

func budgetWithinParent(store *state.State, caller string, input contracts.Input) error {
	if store == nil {
		return nil
	}
	target, ok := store.FindSession(input.Get(paramName))
	if !ok || target.Parent == "" {
		return nil
	}
	parent, ok := store.FindSession(target.Parent)
	if !ok {
		return nil
	}
	for _, budget := range budgetCaps() {
		raw, set := input.Lookup(budget.param)
		ceiling := budget.parent(parent)
		if !set || raw == "" || ceiling <= 0 {
			continue
		}
		asked, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			continue
		}
		if asked <= 0 || asked > ceiling {
			return &accessDeniedError{Reason: fmt.Sprintf(
				"herrscher: session %q may not set %s of %q to %s: its parent %q is capped at %s, and a session never goes above its parent.",
				caller, budget.param, target.Name, raw, parent.Name, strconv.FormatFloat(ceiling, 'f', -1, 64))}
		}
	}
	return nil
}

func (h *hub) announceSelfCall(session, key, subject, detail string) {
	if h.eventPublisher == nil || session == "" {
		return
	}
	text := h.messages.text(h.sessionLanguageSource(session)(), key, map[string]any{"Subject": subject, "Detail": detail})
	h.eventPublisher(session, contracts.Event{T: eventwire.EventNotice, Text: text})
}
