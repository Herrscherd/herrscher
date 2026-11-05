package manager

import "github.com/Herrscherd/herrscher/core/internal/state"

func budgetReason(sessionCost float64, sessionTokens uint64,
	costCap float64, tokenCap uint64,
	cohortCost float64, cohortTokens uint64,
	cohortCostCap float64, cohortTokenCap uint64,
) string {
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

func remaining(limit, spent uint64) uint64 {
	if spent >= limit {
		return 0
	}
	return limit - spent
}

// SessionBudgetGate enforces per-session and cohort budget caps. It re-derives
// usage from the transcript fold (single source of truth) and persists the
// paused reason when a cap trips at a turn boundary; it also answers how much
// token headroom a turn has, so the host can cut a runaway one mid-flight.
// Injected into the host turn loop via the host's budgetGate interface.
type SessionBudgetGate struct {
	h *Handler
}

type sessionUsage struct {
	cost   float64
	tokens uint64
}

// BudgetCheck is the verdict on a live session: why it is capped, and the headroom left before its cap.
type BudgetCheck struct {
	Reason   string
	Headroom uint64
	Capped   bool
}

// BudgetGate returns the gate the host consults to check live sessions against their caps.
// BudgetGate returns the budget gate of this handler's sessions.
func (h *Handler) BudgetGate() *SessionBudgetGate { return &SessionBudgetGate{h: h} }

func (g *SessionBudgetGate) usageFor(s state.Session) sessionUsage {
	entries := state.ReadTranscript(state.TranscriptPath(g.h.partDir, s.Name), 0)
	aggregated := aggregateUsage(entries).usage
	if aggregated == nil {
		return sessionUsage{}
	}
	return sessionUsage{cost: aggregated.Cost, tokens: nonNegativeCount(aggregated.TokensIn + aggregated.TokensOut)}
}

// Check answers both budget questions from one transcript fold: the reason the
// session must pause ("" = it may run), and, for the turn about to start, how
// many tokens it may spend before a token cap trips, with capped false when no
// token cap applies. On a trip it persists PausedReason so the session comes
// back paused across reloads. Reason vocabulary: cost|tokens|cohort_cost|cohort_tokens.
//
// The headroom is what the host watches the live counter against, so a single
// runaway turn is cut mid-flight instead of only being caught at a boundary it
// has already blown past. Cost has no equivalent: a backend reports cost only in
// its terminal result event, so there is no mid-turn number to compare a cost
// cap against.
func (g *SessionBudgetGate) Check(session string) BudgetCheck {
	handler := g.h
	sess, ok := handler.st.FindSession(session)
	if !ok {
		return BudgetCheck{}
	}
	own := g.usageFor(sess)
	spentCost, stk := own.cost, own.tokens
	cohort := cohortTotals(sess, handler.st.SnapshotSessions(), g.usageFor)
	cc, ctk := cohort.cost, cohort.tokens
	reason := budgetReason(spentCost, stk, sess.CostCap, sess.TokenCap, cc, ctk, sess.CohortCostCap, sess.CohortTokenCap)
	if reason != "" {
		handler.warnIfFailed(handler.st.SetPausedReason(session, reason), "pause "+session)
	}
	headroom := ^uint64(0)
	capped := false
	if sess.TokenCap > 0 {
		headroom, capped = min(headroom, remaining(sess.TokenCap, stk)), true
	}
	if sess.CohortTokenCap > 0 {
		headroom, capped = min(headroom, remaining(sess.CohortTokenCap, ctk)), true
	}
	if !capped {
		headroom = 0
	}
	return BudgetCheck{Reason: reason, Headroom: headroom, Capped: capped}
}

func cohortMembers(target state.Session, all []state.Session) []state.Session {
	byName := make(map[string]state.Session, len(all))
	children := make(map[string][]string, len(all))
	for i := range all {
		session := all[i]
		byName[session.Name] = session
		if session.Parent != "" && session.Parent != session.Name {
			children[session.Parent] = append(children[session.Parent], session.Name)
		}
	}
	var members []state.Session
	visited := make(map[string]bool)
	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		member, ok := byName[name]
		if !ok {
			return
		}
		members = append(members, member)
		for _, ch := range children[name] {
			walk(ch)
		}
	}
	walk(state.TreeRoot(all, target.Name))
	return members
}

func nonNegativeCount(count int) uint64 {
	if count < 0 {
		return 0
	}
	return uint64(count)
}
