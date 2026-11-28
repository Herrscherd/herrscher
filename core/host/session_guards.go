package host

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
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
	verbServiceUpdate  = "service update"
	verbEnvSet         = "env set"
	verbEnvUnset       = "env unset"
	verbSetBudget      = "session set-budget"
)

type selfRestarts struct {
	mu    sync.Mutex
	store *state.State
	now   func() time.Time
}

func newSelfRestarts(store *state.State) *selfRestarts {
	return &selfRestarts{store: store, now: time.Now}
}

func restartsTheDaemon(verb string) bool {
	return verb == verbServiceRestart || verb == verbServiceUpdate
}

func (r *selfRestarts) admit(session, verb string) error {
	if r == nil || r.store == nil || !restartsTheDaemon(verb) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if previous, ok := r.store.LastSelfRestart(session); ok && now.Sub(previous) < selfRestartWindow {
		return &accessDeniedError{Reason: fmt.Sprintf(
			"herrscher: session %q already restarted the daemon %s ago; a session restarts it once every %s.",
			session, now.Sub(previous).Round(time.Second), selfRestartWindow)}
	}
	return r.store.RecordSelfRestart(session, now, now.Add(-selfRestartWindow))
}

func (h *hub) guardSessionCall(ctx context.Context, session, verb string, input contracts.Input) error {
	switch verb {
	case verbDriveLs, verbDriveRead, verbDriveWrite:
		return h.guardDrive(ctx, session, verb, input)
	case verbEnvSet, verbEnvUnset:
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

const announcedSubjectRunes = 240

func clippedSubject(subject string) string {
	runes := []rune(subject)
	if len(runes) <= announcedSubjectRunes {
		return subject
	}
	return string(runes[:announcedSubjectRunes]) + "…"
}

func (h *hub) announceSelfCall(session, key, subject, detail string) {
	h.announce(session, key, map[string]any{"Subject": subject, "Detail": detail})
}

func (h *hub) announce(session, key string, params map[string]any) {
	if h.eventPublisher == nil || session == "" {
		return
	}
	text := h.messages.text(h.sessionLanguageSource(session)(), key, params)
	h.eventPublisher(session, contracts.Event{T: eventwire.EventNotice, Text: text})
}

const (
	resumeWait  = 2 * time.Minute
	resumeRetry = 2 * time.Second
)

func resumeAfterSelfRestart(ctx context.Context, store *state.State, seed func(session, task string) bool, text func(session string) string, log *slog.Logger) {
	pending, err := store.TakeRestartResumes()
	if err != nil {
		log.Warn("self restart: the sessions to resume were not read", "err", err)
	}
	deadline := time.Now().Add(resumeWait)
	for len(pending) > 0 {
		pending = slices.DeleteFunc(pending, func(session string) bool { return seed(session, text(session)) })
		if len(pending) == 0 || time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(resumeRetry):
		}
	}
	for _, session := range pending {
		log.Warn("self restart: the session was not resumed, it is not running", "session", session)
	}
}
