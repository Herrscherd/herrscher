// Package manager runs the lifecycle of the sessions a chat channel hosts: creation,
// switching, budgets, renames and the commands that inspect them.
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func (h *Handler) agentCreateRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	raw, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	name := slugify(raw)
	if name == "" || !sessionNameRe.MatchString(name) {
		return contracts.Result{}, &InvalidValueError{Field: nameParam, Value: raw, Hint: "use letters, digits, - or _ (max 64)"}
	}
	soul, _ := input.Lookup("soul")
	mcp, _ := input.Lookup("mcp")
	backend, _ := input.Lookup("backend")
	cmd, _ := input.Lookup("cmd")
	host, _ := input.Lookup("host")
	tagsRaw, _ := input.Lookup("tags")
	tags := strings.Fields(strings.ReplaceAll(tagsRaw, ",", " "))
	created, err := h.agents.Create(agent.CreateSpec{Name: name, Soul: soul, MCP: mcp, Backend: backend, Cmd: cmd, Host: strings.TrimSpace(host), Tags: tags})
	if err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(h.text(clitext.HostAgentCreated, clitext.Params{clitext.ParamName: created.Name, clitext.ParamHome: created.Home})), nil
}

func (h *Handler) agentListRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	agents, err := h.agents.List()
	if err != nil {
		return contracts.Result{}, err
	}
	if input.JSON {
		type row struct {
			Name    string   `json:"name"`
			Home    string   `json:"home"`
			Tags    []string `json:"tags"`
			Backend string   `json:"backend"`
		}
		rows := make([]row, 0, len(agents))
		for i := range agents {
			listed := &agents[i]
			tags := listed.Tags
			if tags == nil {
				tags = []string{}
			}
			rows = append(rows, row{Name: listed.Name, Home: listed.Home, Tags: tags, Backend: listed.Backend})
		}
		buf, err := json.Marshal(rows)
		if err != nil {
			return contracts.Result{}, err
		}
		return contracts.DataResult(string(buf)), nil
	}
	if len(agents) == 0 {
		return contracts.DataResult(h.text(clitext.HostAgentListEmpty, nil)), nil
	}
	var out strings.Builder
	out.WriteString(h.text(clitext.HostAgentListHeader, nil) + "\n")
	for i := range agents {
		a := &agents[i]
		fmt.Fprintf(&out, "• %s (%#q)\n", a.Name, a.Home)
	}
	return contracts.DataResult(out.String()), nil
}

func (h *Handler) agentShowRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	candidate, ok := h.agents.Get(slugify(name))
	if !ok {
		return contracts.Result{}, &agent.NotFoundError{Name: name}
	}
	soul, err := os.ReadFile(filepath.Join(candidate.Home, "SOUL.md"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return contracts.Result{}, h.labelled("read soul", clitext.HostErrReadSoul, err)
	}
	reply, err := h.agents.Reply(candidate.Name)
	if err != nil {
		return contracts.Result{}, h.labelled("read reply", clitext.HostErrReadVoice, err)
	}
	channels, err := h.agents.Channels(candidate.Name)
	if err != nil {
		return contracts.Result{}, h.labelled("read channels", clitext.HostErrReadVoice, err)
	}
	tokens := h.tokenPresence(candidate.Name)
	if input.JSON {
		tags := candidate.Tags
		if tags == nil {
			tags = []string{}
		}
		channelRefs := make([]string, 0, len(channels))
		for _, channel := range channels {
			channelRefs = append(channelRefs, channel.String())
		}
		buf, err := json.Marshal(struct {
			Name     string          `json:"name"`
			Tags     []string        `json:"tags"`
			Backend  string          `json:"backend"`
			Soul     string          `json:"soul"`
			Reply    string          `json:"reply"`
			Channels []string        `json:"channels"`
			Tokens   map[string]bool `json:"tokens"`
		}{candidate.Name, tags, candidate.Backend, string(soul), reply.String(), channelRefs, tokens})
		if err != nil {
			return contracts.Result{}, err
		}
		return contracts.DataResult(string(buf)), nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%s (%#q)\n", candidate.Name, candidate.Home)
	out.WriteString(h.text(clitext.HostAgentShowReply, clitext.Params{clitext.ParamRule: reply.String()}) + "\n")
	out.WriteString(h.channelsLine(channels) + "\n")
	for _, kind := range sortedKinds(tokens) {
		state := h.text(clitext.HostAgentTokenNotSet, nil)
		if tokens[kind] {
			state = h.text(clitext.HostAgentTokenSet, nil)
		}
		out.WriteString(h.text(clitext.HostAgentShowToken, clitext.Params{clitext.ParamKind: kind, clitext.ParamState: state}) + "\n")
	}
	out.WriteString("\n" + string(soul))
	return contracts.DataResult(out.String()), nil
}

func (h *Handler) tokenPresence(name string) map[string]bool {
	tokens := make(map[string]bool, len(h.gatewayAdmins))
	for kind := range h.gatewayAdmins {
		tokens[kind] = h.botTokens != nil && h.botTokens.HasToken(kind, name)
	}
	return tokens
}

func sortedKinds(tokens map[string]bool) []string {
	return slices.Sorted(maps.Keys(tokens))
}

func (h *Handler) channelsLine(channels []agent.Channel) string {
	if len(channels) == 0 {
		return h.text(clitext.HostAgentShowNoChannels, nil)
	}
	refs := make([]string, 0, len(channels))
	for _, channel := range channels {
		refs = append(refs, channel.String())
	}
	return h.text(clitext.HostAgentShowChannels, clitext.Params{clitext.ParamChannels: strings.Join(refs, ", ")})
}

func (h *Handler) agentSetReplyRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	operands := slices.Clone(input.Rest)
	name, ok := input.Lookup(nameParam)
	if !ok {
		if len(operands) == 0 {
			return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
		}
		name, operands = operands[0], operands[1:]
	}
	ruleText, ok := input.Lookup("rule")
	if !ok && len(operands) > 0 {
		ruleText, operands = operands[0], operands[1:]
	}
	rule, err := contracts.ParseReplyRule(ruleText)
	if err != nil {
		return contracts.Result{}, h.localized(contracts.ErrUnknownReplyRule, clitext.HostErrUnknownReplyRule)
	}
	channels, err := h.parseChannels(operands)
	if err != nil {
		return contracts.Result{}, err
	}
	agentName := slugify(name)
	if err := h.agents.SetReply(agentName, rule); err != nil {
		return contracts.Result{}, err
	}
	if len(operands) > 0 {
		if err := h.agents.SetChannels(agentName, channels); err != nil {
			return contracts.Result{}, err
		}
	}
	return contracts.DataResult(h.text(clitext.HostAgentReplySet, clitext.Params{clitext.ParamName: agentName, clitext.ParamRule: rule.String()})), nil
}

func (h *Handler) parseChannels(texts []string) ([]agent.Channel, error) {
	channels := make([]agent.Channel, 0, len(texts))
	for _, text := range texts {
		channel, err := agent.ParseChannel(text)
		if err != nil {
			return nil, localize(h.messages, h.currentLanguage(), agent.ErrBadChannel.Error(), clitext.HostErrBadChannel, clitext.Params{clitext.ParamChannel: text}, agent.ErrBadChannel)
		}
		channels = append(channels, channel)
	}
	return channels, nil
}

func (h *Handler) agentSetSoulRun(_ context.Context, input contracts.Input) (contracts.Result, error) {
	name, ok := input.Lookup(nameParam)
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingName, clitext.HostErrMissingName)
	}
	soul, ok := input.Lookup("soul")
	if !ok {
		return contracts.Result{}, h.localized(ErrMissingSoul, clitext.HostErrMissingSoul)
	}
	if err := h.agents.SetSoul(slugify(name), soul); err != nil {
		return contracts.Result{}, err
	}
	return contracts.DataResult(h.text(clitext.HostAgentSoulUpdated, clitext.Params{clitext.ParamName: slugify(name)})), nil
}
