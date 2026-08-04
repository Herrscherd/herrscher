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

// remaining is the distance left to a cap, floored at zero — a cap already
// blown leaves no headroom rather than wrapping to a huge unsigned number.
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

// BudgetGate returns a gate bound to this handler's session store + transcript
// dir. The returned *SessionBudgetGate satisfies host's (private) budgetGate
// interface structurally via Check.
func (h *Handler) BudgetGate() *SessionBudgetGate { return &SessionBudgetGate{h: h} }

// usageFor folds one session's transcript into (cost, tokens). Every budget
// question derives from it rather than from a running counter, because the
// transcript is the single source of truth for what a session has spent.
func (g *SessionBudgetGate) usageFor(s state.Session) (float64, uint64) {
	entries := state.ReadTranscript(state.TranscriptPath(g.h.partDir, s.Name), 0)
	u, _ := aggregateUsage(entries)
	if u == nil {
		return 0, 0
	}
	return u.Cost, uint64(u.TokensIn + u.TokensOut)
}

// Check answers both budget questions from one transcript fold: the reason the
// session must pause ("" = it may run), and — for the turn about to start — how
// many tokens it may spend before a token cap trips, with capped false when no
// token cap applies. On a trip it persists PausedReason so the session comes
// back paused across reloads. Reason vocabulary: cost|tokens|cohort_cost|cohort_tokens.
//
// The headroom is what the host watches the live counter against, so a single
// runaway turn is cut mid-flight instead of only being caught at a boundary it
// has already blown past. Cost has no equivalent: a backend reports cost only in
// its terminal result event, so there is no mid-turn number to compare a cost
// cap against.
func (g *SessionBudgetGate) Check(session string) (reason string, headroom uint64, capped bool) {
	h := g.h
	sess, ok := h.st.FindSession(session)
	if !ok {
		return "", 0, false
	}
	sc, stk := g.usageFor(sess)
	cc, ctk := cohortTotals(sess, h.st.SnapshotSessions(), g.usageFor)
	reason = budgetReason(sc, stk, sess.CostCap, sess.TokenCap, cc, ctk, sess.CohortCostCap, sess.CohortTokenCap)
	if reason != "" {
		_ = h.st.SetPausedReason(session, reason)
	}
	headroom = ^uint64(0)
	if sess.TokenCap > 0 {
		headroom, capped = min(headroom, remaining(sess.TokenCap, stk)), true
	}
	if sess.CohortTokenCap > 0 {
		headroom, capped = min(headroom, remaining(sess.CohortTokenCap, ctk)), true
	}
	if !capped {
		headroom = 0
	}
	return reason, headroom, capped
}

// cohortMembers returns every session in the parent forest that target belongs
// to: the root ancestor and everything transitively reachable from it via
// Parent. Same traversal as cohortTotals, but collects the Session values
// instead of folding usage. Cycles are cut with a visited set, mirroring
// cohortTotals' guard.
func cohortMembers(target state.Session, all []state.Session) []state.Session {
	byName := make(map[string]state.Session, len(all))
	children := make(map[string][]string, len(all))
	for _, s := range all {
		byName[s.Name] = s
		if s.Parent != "" && s.Parent != s.Name {
			children[s.Parent] = append(children[s.Parent], s.Name)
		}
	}
	// Climb to the root ancestor.
	root := target
	seen := map[string]bool{root.Name: true}
	for root.Parent != "" && root.Parent != root.Name {
		p, ok := byName[root.Parent]
		if !ok || seen[p.Name] {
			break
		}
		seen[p.Name] = true
		root = p
	}
	// Walk the whole subtree from root, collecting members.
	var members []state.Session
	visited := map[string]bool{}
	var walk func(name string)
	walk = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		s, ok := byName[name]
		if !ok {
			return
		}
		members = append(members, s)
		for _, ch := range children[name] {
			walk(ch)
		}
	}
	walk(root.Name)
	return members
}
