package manager

import (
	"context"
	"strconv"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type budgetPatch struct {
	costCap        *float64
	tokenCap       *uint64
	cohortCostCap  *float64
	cohortTokenCap *uint64
}

func fptr(f float64) *float64 { return &f }
func uptr(u uint64) *uint64   { return &u }

func applyBudget(session state.Session, patch budgetPatch, curCost float64, curTokens uint64) state.Session {
	if patch.costCap != nil {
		session.CostCap = *patch.costCap
	}
	if patch.tokenCap != nil {
		session.TokenCap = *patch.tokenCap
	}
	if patch.cohortCostCap != nil {
		session.CohortCostCap = *patch.cohortCostCap
	}
	if patch.cohortTokenCap != nil {
		session.CohortTokenCap = *patch.cohortTokenCap
	}
	if session.PausedReason == "cost" && (session.CostCap == 0 || curCost < session.CostCap) {
		session.PausedReason = ""
	}
	if session.PausedReason == "tokens" && (session.TokenCap == 0 || curTokens < session.TokenCap) {
		session.PausedReason = ""
	}
	return session
}

func (h *Handler) sessionSetBudgetRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	sess, ok := h.st.FindSession(name)
	if !ok {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	patch, err := parseBudgetPatch(input)
	if err != nil {
		return contracts.Result{}, err
	}
	targets := []state.Session{sess}
	if scope, _ := input.Lookup("scope"); scope == "cohort" {
		targets = cohortMembers(sess, h.st.SnapshotSessions())
	}
	for i := range targets {
		if applyErr := h.applyBudgetTo(targets[i], patch); applyErr != nil {
			return contracts.Result{}, applyErr
		}
	}
	return contracts.DataResult(h.text(clitext.SessionBudgetUpdated, clitext.Params{clitext.ParamCount: len(targets)})), nil
}

func parseBudgetPatch(input contracts.Input) (budgetPatch, error) {
	var patch budgetPatch
	if err := setFloatCap(input, "cost_cap", &patch.costCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setCountCap(input, "token_cap", &patch.tokenCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setFloatCap(input, "cohort_cost_cap", &patch.cohortCostCap); err != nil {
		return budgetPatch{}, err
	}
	if err := setCountCap(input, "cohort_token_cap", &patch.cohortTokenCap); err != nil {
		return budgetPatch{}, err
	}
	return patch, nil
}

func setFloatCap(input contracts.Input, key string, dst **float64) error {
	raw, ok := input.Lookup(key)
	if !ok || raw == "" {
		return nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return &InvalidValueError{Field: key, Value: raw}
	}
	*dst = fptr(parsed)
	return nil
}

func setCountCap(input contracts.Input, key string, dst **uint64) error {
	raw, ok := input.Lookup(key)
	if !ok || raw == "" {
		return nil
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return &InvalidValueError{Field: key, Value: raw}
	}
	*dst = uptr(nonNegativeCount(count))
	return nil
}

func (h *Handler) applyBudgetTo(target state.Session, patch budgetPatch) error {
	usageFor := h.usageOf
	usage := usageFor(target)
	cohort := cohortTotals(target, h.st.SnapshotSessions(), usageFor)
	updated := applyBudget(target, patch, usage.cost, usage.tokens)
	releaseCohortPause(&updated, cohort)
	if err := h.persisted(func() error {
		return h.st.SetBudget(updated.Name, updated.CostCap, updated.TokenCap, updated.CohortCostCap, updated.CohortTokenCap, updated.PausedReason)
	}); err != nil {
		return h.keyed(clitext.HostSessionBudgetPersist, clitext.Params{clitext.ParamName: updated.Name, clitext.ParamErr: err.Error()}, err)
	}
	return nil
}

func releaseCohortPause(updated *state.Session, cohort sessionUsage) {
	if updated.PausedReason == "cohort_cost" && (updated.CohortCostCap == 0 || cohort.cost < updated.CohortCostCap) {
		updated.PausedReason = ""
	}
	if updated.PausedReason == "cohort_tokens" && (updated.CohortTokenCap == 0 || cohort.tokens < updated.CohortTokenCap) {
		updated.PausedReason = ""
	}
}
