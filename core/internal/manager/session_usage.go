package manager

import (
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func cohortTotals(target state.Session, all []state.Session,
	usageFor func(state.Session) sessionUsage,
) sessionUsage {
	byName := make(map[string]state.Session, len(all))
	children := make(map[string][]string, len(all))
	for i := range all {
		session := all[i]
		byName[session.Name] = session
		if session.Parent != "" && session.Parent != session.Name {
			children[session.Parent] = append(children[session.Parent], session.Name)
		}
	}
	var totalCost float64
	var totalTokens uint64
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
		usage := usageFor(member)
		totalCost += usage.cost
		totalTokens += usage.tokens
		for _, ch := range children[name] {
			walk(ch)
		}
	}
	walk(state.TreeRoot(all, target.Name))
	return sessionUsage{cost: totalCost, tokens: totalTokens}
}

type transcriptUsage struct {
	usage *usageJSON
	task  string
}

func aggregateUsage(entries []state.TranscriptEntry) transcriptUsage {
	if len(entries) == 0 {
		return transcriptUsage{}
	}
	summary := &usageJSON{StartedAt: entries[0].Ts}
	var task string
	for idx := range entries {
		entry := &entries[idx]
		switch entry.Role {
		case "assistant":
			summary.Turns++
			summary.Cost += entry.Cost
			summary.TokensIn += entry.TokensIn
			summary.TokensOut += entry.TokensOut
			summary.CacheRead += entry.CacheRead
			summary.CacheCreate += entry.CacheCreate
		case "user":
			task = entry.Text
		}
	}
	return transcriptUsage{usage: summary, task: task}
}

func (h *Handler) attachUsage(row *sessionJSON) {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, row.Name), 0)
	if agg := aggregateUsage(entries); agg.usage != nil {
		row.Usage = agg.usage
		if agg.task != "" {
			row.Task = agg.task
		}
	}
}

func (h *Handler) usageOf(s state.Session) sessionUsage {
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, s.Name), 0)
	usage := aggregateUsage(entries).usage
	if usage == nil {
		return sessionUsage{}
	}
	return sessionUsage{cost: usage.Cost, tokens: nonNegativeCount(usage.TokensIn + usage.TokensOut)}
}
