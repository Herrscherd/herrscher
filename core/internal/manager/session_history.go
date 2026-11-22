package manager

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

const (
	sinceParam    = "since"
	roleUser      = "user"
	roleAssistant = "assistant"
	moneyDecimals = 4
)

type historyTurn struct {
	TurnID      string   `json:"turn_id"`
	Started     string   `json:"started,omitempty"`
	Prompt      string   `json:"prompt,omitempty"`
	Reply       string   `json:"reply,omitempty"`
	Notices     []string `json:"notices,omitempty"`
	Interrupted bool     `json:"interrupted,omitempty"`
	TokensIn    int      `json:"tokens_in,omitempty"`
	TokensOut   int      `json:"tokens_out,omitempty"`
	CacheRead   int      `json:"cache_read,omitempty"`
	CacheCreate int      `json:"cache_create,omitempty"`
	DurMs       int      `json:"dur_ms,omitempty"`
	Cost        float64  `json:"cost,omitempty"`
}

func (h *Handler) sessionHistoryRun(ctx context.Context, input contracts.Input) (contracts.Result, error) {
	raw, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	name := slugify(raw)
	var turns []historyTurn
	if name != "" && sessionNameRe.MatchString(name) {
		turns = groupTurns(state.ReadTranscript(state.TranscriptPath(h.partDir, name), 0))
	}
	if since, given := input.Lookup(sinceParam); given {
		after, err := turnsAfter(turns, since)
		if err != nil {
			return contracts.Result{}, err
		}
		turns = after
	}
	if input.JSON {
		if turns == nil {
			turns = []historyTurn{}
		}
		payload, err := json.Marshal(turns)
		return contracts.DataResult(string(payload)), err
	}
	lang := clitext.CallerLanguage(ctx, h.currentLanguage)
	if len(turns) == 0 {
		return contracts.DataResult(h.textIn(lang, clitext.SessionHistoryEmpty, nil)), nil
	}
	return contracts.DataResult(h.renderHistory(turns, lang)), nil
}

func groupTurns(entries []state.TranscriptEntry) []historyTurn {
	var turns []historyTurn
	for idx := range entries {
		entry := entries[idx]
		if startsTurn(turns, entry) {
			turns = append(turns, historyTurn{TurnID: entry.TurnID, Started: entry.Ts})
		}
		absorb(&turns[len(turns)-1], entry)
	}
	for idx := range turns {
		if turns[idx].TurnID == "" {
			turns[idx].TurnID = strconv.Itoa(idx + 1)
		}
	}
	return turns
}

func startsTurn(turns []historyTurn, entry state.TranscriptEntry) bool {
	if len(turns) == 0 || entry.Role == roleUser {
		return true
	}
	current := turns[len(turns)-1]
	return entry.TurnID != "" && current.TurnID != "" && entry.TurnID != current.TurnID
}

func absorb(turn *historyTurn, entry state.TranscriptEntry) {
	if turn.TurnID == "" {
		turn.TurnID = entry.TurnID
	}
	switch {
	case entry.Kind == state.KindTurnInterrupted:
		turn.Interrupted = true
	case entry.Role == roleUser:
		turn.Prompt = joinText(turn.Prompt, entry.ShownText())
	case entry.Role == roleAssistant:
		turn.Reply = joinText(turn.Reply, entry.ShownText())
		turn.TokensIn += entry.TokensIn
		turn.TokensOut += entry.TokensOut
		turn.CacheRead += entry.CacheRead
		turn.CacheCreate += entry.CacheCreate
		turn.DurMs += entry.DurMs
		turn.Cost += entry.Cost
	default:
		turn.Notices = append(turn.Notices, entry.ShownText())
	}
}

func joinText(have, more string) string {
	if have == "" {
		return more
	}
	return have + "\n" + more
}

func turnsAfter(turns []historyTurn, since string) ([]historyTurn, error) {
	for idx := range turns {
		if turns[idx].TurnID == since {
			return turns[idx+1:], nil
		}
	}
	return nil, &InvalidValueError{Field: sinceParam, Value: since}
}

func (h *Handler) renderHistory(turns []historyTurn, lang i18n.Language) string {
	var out strings.Builder
	for idx := range turns {
		turn := turns[idx]
		out.WriteString(h.textIn(lang, clitext.SessionHistoryTurn, clitext.Params{clitext.ParamID: turn.TurnID, clitext.ParamTime: turn.Started}) + "\n")
		if turn.Prompt != "" {
			out.WriteString(h.textIn(lang, clitext.SessionHistoryPrompt, clitext.Params{clitext.ParamMessage: turn.Prompt}) + "\n")
		}
		if turn.Reply != "" {
			out.WriteString(h.textIn(lang, clitext.SessionHistoryReply, clitext.Params{clitext.ParamMessage: turn.Reply}) + "\n")
		}
		for _, notice := range turn.Notices {
			out.WriteString(h.textIn(lang, clitext.SessionHistoryNotice, clitext.Params{clitext.ParamMessage: notice}) + "\n")
		}
		if turn.Interrupted {
			out.WriteString(clitext.RenderNotice(lang, contracts.Notice{Code: contracts.CodeTurnInterrupted}) + "\n")
		}
		if turn.DurMs != 0 || turn.TokensOut != 0 || turn.Cost != 0 {
			out.WriteString(h.textIn(lang, clitext.SessionHistoryUsage, clitext.Params{
				clitext.ParamDuration: clitext.FormatDuration(lang, time.Duration(turn.DurMs)*time.Millisecond),
				clitext.ParamIn:       clitext.FormatNumber(lang, float64(turn.TokensIn), 0),
				clitext.ParamOut:      clitext.FormatNumber(lang, float64(turn.TokensOut), 0),
				clitext.ParamCost:     "$" + clitext.FormatNumber(lang, turn.Cost, moneyDecimals),
			}) + "\n")
		}
		out.WriteString("\n")
	}
	return out.String()
}
