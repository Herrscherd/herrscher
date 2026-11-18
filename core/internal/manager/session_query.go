package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

type sessionJSON struct {
	ID           string            `json:"id"`
	Incarnation  string            `json:"incarnation"`
	Name         string            `json:"name"`
	Agent        string            `json:"agent"`
	Project      string            `json:"project"`
	Status       string            `json:"status"`
	Worktree     string            `json:"worktree"`
	Gateways     []string          `json:"gateways"`
	Parent       string            `json:"parent,omitempty"`
	Coordination *coordinationJSON `json:"coordination,omitempty"`
	Resumable    bool              `json:"resumable"`
	Usage        *usageJSON        `json:"usage,omitempty"`
	Task         string            `json:"task,omitempty"`
}

type usageJSON struct {
	Cost           float64 `json:"cost,omitempty"`
	TokensIn       int     `json:"tokens_in,omitempty"`
	TokensOut      int     `json:"tokens_out,omitempty"`
	CacheRead      int     `json:"cache_read,omitempty"`
	CacheCreate    int     `json:"cache_create,omitempty"`
	Turns          int     `json:"turns,omitempty"`
	StartedAt      string  `json:"started_at,omitempty"`
	CostCap        float64 `json:"cost_cap,omitempty"`
	TokenCap       uint64  `json:"token_cap,omitempty"`
	CohortCostCap  float64 `json:"cohort_cost_cap,omitempty"`
	CohortTokenCap uint64  `json:"cohort_token_cap,omitempty"`
	PausedReason   string  `json:"paused_reason,omitempty"`
}

func (h *Handler) stampBudget(row *sessionJSON, session state.Session) {
	if session.CostCap == 0 && session.TokenCap == 0 && session.CohortCostCap == 0 &&
		session.CohortTokenCap == 0 && session.PausedReason == "" {
		return
	}
	if row.Usage == nil {
		row.Usage = &usageJSON{}
	}
	row.Usage.CostCap = session.CostCap
	row.Usage.TokenCap = session.TokenCap
	row.Usage.CohortCostCap = session.CohortCostCap
	row.Usage.CohortTokenCap = session.CohortTokenCap
	row.Usage.PausedReason = session.PausedReason
}

type coordinationJSON struct {
	Role     string `json:"role"`
	Lead     string `json:"lead"`
	Reported int    `json:"reported"`
	Expected int    `json:"expected"`
	Complete bool   `json:"complete"`
}

func sessionJSONRow(session state.Session) sessionJSON {
	gateways := session.BoundGateways()
	if gateways == nil {
		gateways = []string{}
	}
	status := "running"
	if session.PausedReason != "" {
		status = "paused"
	}
	if session.Archived {
		status = "archived"
	}
	return sessionJSON{
		ID: session.Name, Incarnation: session.Incarnation, Name: session.Name, Agent: session.Agent, Project: session.Project, Status: status,
		Worktree: session.Worktree, Gateways: gateways, Parent: session.Parent,
		Resumable: session.ResumeToken != "",
	}
}

func (h *Handler) sessionListRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	sessions := h.st.SnapshotSessions()
	if input.JSON {
		rows := make([]sessionJSON, 0, len(sessions))
		for i := range sessions {
			session := sessions[i]
			row := sessionJSONRow(session)
			h.attachUsage(&row)
			h.stampBudget(&row, session)
			if h.coord != nil {
				if view, ok := h.coord.CoordinationView(session.Name); ok {
					row.Coordination = &coordinationJSON{
						Role: view.Role, Lead: view.Lead, Reported: view.Reported,
						Expected: view.Expected, Complete: view.Complete,
					}
				}
			}
			rows = append(rows, row)
		}
		b, err := json.Marshal(rows)
		return contracts.DataResult(string(b)), err
	}
	if len(sessions) == 0 {
		return contracts.DataResult(h.text(clitext.SessionListEmpty, nil)), nil
	}
	var out strings.Builder
	out.WriteString(h.text(clitext.SessionListHeader, nil) + "\n")
	for i := range sessions {
		s := sessions[i]
		fmt.Fprintf(&out, "• %s (%s) %s\n", s.Name, s.Type, h.adminFor(s).ChannelRef(s.ChannelID))
	}
	return contracts.DataResult(out.String()), nil
}

func (h *Handler) sessionWhoRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	sess, exists := h.st.FindSession(name)
	if !exists {
		return contracts.Result{}, &SessionNotFoundError{Name: name}
	}
	if input.JSON {
		row := sessionJSONRow(sess)
		h.attachUsage(&row)
		b, err := json.Marshal(row)
		return contracts.DataResult(string(b)), err
	}
	ids := state.ReadParticipants(state.ParticipantsPath(h.partDir, name))
	if len(ids) == 0 {
		return contracts.DataResult(h.text(clitext.SessionWhoEmpty, nil)), nil
	}
	var out strings.Builder
	out.WriteString(h.text(clitext.SessionWhoHeader, clitext.Params{clitext.ParamName: name}) + "\n")
	for _, id := range ids {
		fmt.Fprintf(&out, "• <@%s>\n", id)
	}
	return contracts.DataResult(out.String()), nil
}

func (h *Handler) sessionLogRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	raw, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		if input.JSON {
			return contracts.DataResult("[]"), nil
		}
		return contracts.DataResult(h.text(clitext.SessionLogEmpty, nil)), nil
	}
	limit := sessionLogTranscriptCap
	if lraw, ok := input.Lookup("limit"); ok {
		if n, err := strconv.Atoi(lraw); err == nil && n > 0 {
			limit = n
		}
	}
	entries := state.ReadTranscript(state.TranscriptPath(h.partDir, name), limit)
	if input.JSON {
		if entries == nil {
			entries = []state.TranscriptEntry{}
		}
		b, err := json.Marshal(entries)
		return contracts.DataResult(string(b)), err
	}
	var out strings.Builder
	for i := range entries {
		shown := entries[i].ShownText()
		if shown == "" {
			continue
		}
		fmt.Fprintf(&out, "%s %s\n", entries[i].Role, shown)
	}
	if out.Len() == 0 {
		return contracts.DataResult(h.text(clitext.SessionLogEmpty, nil)), nil
	}
	return contracts.DataResult(out.String()), nil
}
