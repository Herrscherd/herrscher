package manager

import "github.com/Herrscherd/herrscher/core/internal/state"

// budgetReason returns the reason a session must pause AFTER the current turn,
// or "" if it may continue. Session caps are checked before cohort caps so a
// session-level trip names its own unit. A zero cap means uncapped.
func budgetReason(sessionCost float64, sessionTokens uint64,
	costCap float64, tokenCap uint64,
	cohortCost float64, cohortTokens uint64,
	cohortCostCap float64, cohortTokenCap uint64) string {
	if costCap > 0 && sessionCost >= costCap {
		return "cost"
	}
	if tokenCap > 0 && sessionTokens >= tokenCap {
		return "tokens"
	}
	if cohortCostCap > 0 && cohortCost >= cohortCostCap {
		return "cohort_cost"
	}
	if cohortTokenCap > 0 && cohortTokens >= cohortTokenCap {
		return "cohort_tokens"
	}
	return ""
}

// SessionBudgetGate enforces per-session and cohort budget caps at the turn
// boundary. It re-derives usage from the transcript fold (single source of
// truth) and persists the paused reason when a cap trips. Injected into the
// host turn loop via the host's budgetGate interface.
type SessionBudgetGate struct {
	h *Handler
}

// BudgetGate returns a gate bound to this handler's session store + transcript
// dir. The returned *SessionBudgetGate satisfies host's (private) budgetGate
// interface structurally via CheckAfterTurn.
func (h *Handler) BudgetGate() *SessionBudgetGate { return &SessionBudgetGate{h: h} }

// CheckAfterTurn returns the reason the session must pause after the current
// turn ("" = continue). On a trip it persists PausedReason so the session comes
// back paused across reloads. Reason vocabulary: cost|tokens|cohort_cost|cohort_tokens.
func (g *SessionBudgetGate) CheckAfterTurn(session string) string {
	h := g.h
	sess, ok := h.st.FindSession(session)
	if !ok {
		return ""
	}
	usageFor := func(s state.Session) (float64, uint64) {
		entries := state.ReadTranscript(state.TranscriptPath(h.partDir, s.Name), 0)
		u, _ := aggregateUsage(entries)
		if u == nil {
			return 0, 0
		}
		return u.Cost, uint64(u.TokensIn + u.TokensOut)
	}
	sc, stk := usageFor(sess)
	cc, ctk := cohortTotals(sess, h.st.SnapshotSessions(), usageFor)
	reason := budgetReason(sc, stk, sess.CostCap, sess.TokenCap, cc, ctk, sess.CohortCostCap, sess.CohortTokenCap)
	if reason != "" {
		_ = h.st.SetPausedReason(session, reason)
	}
	return reason
}
