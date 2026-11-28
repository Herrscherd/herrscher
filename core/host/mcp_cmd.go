package host

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"strconv"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/agent"
	"github.com/Herrscherd/herrscher/core/internal/manager"
	"github.com/Herrscherd/herrscher/core/internal/mcpweight"
)

const (
	paramMCPDeep = "deep"
	paramMCPRole = "role"
	nsMCP        = "mcp"
	verbWeight   = "weight"
)

// MCPWeightDeadline is how long the operator waits for the daemon's mcp weight report: the whole measure budget plus the margin for the servers still stopping.
const MCPWeightDeadline = mcpweight.DefaultBudget + mcpweight.ReplyMargin

func isReadOnlyCommand(args []string) bool {
	return len(args) >= 2 && args[0] == nsMCP && args[1] == verbWeight
}

type mcpWeightRequest struct {
	agent  string
	role   string
	deep   bool
	asJSON bool
}

func mcpWeightVerb(cliMessages *clitext.Messages, lang languageSource, agents agentLookup, measurer *mcpweight.Measurer) contracts.Cmd {
	return contracts.New(nsMCP, verbWeight).
		Help(clitext.CmdHelpToolWeight).
		ValueParam(paramAgent, clitext.CmdParamToolWeightAgent, true).
		Param(paramMCPDeep, clitext.CmdParamToolWeightDeep, false).
		ValueParam(paramMCPRole, clitext.CmdParamToolWeightRole, false).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			req := mcpWeightRequest{agent: in.Get(paramAgent), role: in.Get(paramMCPRole), deep: in.Bool(paramMCPDeep), asJSON: in.JSON}
			return cli.Data(mcpWeightReport(ctx, cliMessages, lang(), agents, measurer, req))
		})
}

func mcpWeightReport(ctx context.Context, cliMessages *clitext.Messages, lang i18n.Language, agents agentLookup, measurer *mcpweight.Measurer, req mcpWeightRequest) (string, error) {
	found, ok := agents.Get(req.agent)
	if !ok {
		return "", &UnknownError{Kind: kindAgent, Name: req.agent}
	}
	servers, selection, err := weightServers(found, req.role)
	if err != nil {
		return "", localizedMCPProfileError(cliMessages, lang, req.agent, err)
	}
	results := mcpWeigh(ctx, measurer, servers, req.deep)
	if req.asJSON {
		body, encodeErr := json.Marshal(results)
		return string(body), encodeErr
	}
	return renderMCPWeight(cliMessages, lang, req.agent, results) + mcpProfileNote(cliMessages, lang, selection), nil
}

func weightServers(found agent.Agent, role string) ([]agent.MCPServer, agent.MCPSelection, error) {
	if role == "" {
		servers, err := mcpweight.ReadServers(found.MCPConfigPath())
		return servers, agent.MCPSelection{}, err
	}
	selection, err := found.SelectMCP(role)
	if errors.Is(err, fs.ErrNotExist) {
		servers, readErr := mcpweight.ReadServers(found.MCPConfigPath())
		return servers, agent.MCPSelection{}, readErr
	}
	if err != nil {
		return nil, agent.MCPSelection{}, err
	}
	servers, err := agent.ParseMCPServers(selection.Document)
	return servers, selection, err
}

func localizedMCPProfileError(cliMessages *clitext.Messages, lang i18n.Language, agentName string, err error) error {
	key, params, ok := manager.MCPProfileProblem(err, agentName)
	if !ok {
		return err
	}
	text, textErr := cliMessages.Text(i18n.Resolve(lang), key, params)
	if textErr != nil {
		return err
	}
	return localizedHostError{text: text, cause: err}
}

func mcpProfileNote(cliMessages *clitext.Messages, lang i18n.Language, selection agent.MCPSelection) string {
	if selection.Role == "" {
		return ""
	}
	text := replyText(cliMessages, lang)
	if !selection.Applied {
		return "\n" + text.Text(clitext.ToolWeightNoProfile, clitext.Params{clitext.ParamRole: selection.Role})
	}
	return "\n" + text.Text(clitext.ToolWeightProfile, clitext.Params{
		clitext.ParamRole:    selection.Role,
		clitext.ParamKept:    joinOrNone(text, selection.Kept),
		clitext.ParamDropped: joinOrNone(text, selection.Dropped),
	})
}

func joinOrNone(text *clitext.Translator, names []string) string {
	if len(names) == 0 {
		return text.Text(clitext.ToolWeightProfileNone, nil)
	}
	return strings.Join(names, ", ")
}

func mcpWeigh(ctx context.Context, measurer *mcpweight.Measurer, servers []agent.MCPServer, deep bool) []mcpweight.Result {
	if deep {
		return measurer.MeasureAll(ctx, servers, mcpweight.DefaultBudget)
	}
	results := make([]mcpweight.Result, 0, len(servers))
	for _, server := range servers {
		results = append(results, mcpweight.Describe(server))
	}
	return results
}

func renderMCPWeight(cliMessages *clitext.Messages, lang i18n.Language, agentName string, results []mcpweight.Result) string {
	text := replyText(cliMessages, lang)
	if len(results) == 0 {
		return text.Text(clitext.ToolWeightNone, nil)
	}
	var body strings.Builder
	_, _ = body.WriteString(text.Text(clitext.ToolWeightHeader, clitext.Params{clitext.ParamAgent: agentName}) + "\n")
	for _, result := range results {
		_, _ = body.WriteString(mcpWeightLine(text, result) + "\n")
	}
	_, _ = body.WriteString(text.Text(clitext.ToolWeightEstimate, nil))
	return body.String()
}

func mcpWeightLine(text *clitext.Translator, result mcpweight.Result) string {
	if result.Measured {
		return mcpMeasuredLine(text, result)
	}
	line := text.Text(clitext.ToolWeightFailed, clitext.Params{
		clitext.ParamName:      result.Name,
		clitext.ParamTransport: result.Transport,
		clitext.ParamReason:    text.Text(mcpWeightCauseKey(result.Cause), nil),
	})
	return withServerStderr(text, line, result.Stderr)
}

func mcpMeasuredLine(text *clitext.Translator, result mcpweight.Result) string {
	line := text.Text(clitext.ToolWeightMeasured, clitext.Params{
		clitext.ParamName:      result.Name,
		clitext.ParamTransport: result.Transport,
		clitext.ParamTools:     result.Tools,
		clitext.ParamBytes:     result.Bytes,
		clitext.ParamTokens:    result.Tokens,
	})
	if result.Cause == "" {
		return line
	}
	stopped := text.Text(clitext.ToolWeightStopped, clitext.Params{
		clitext.ParamName:   result.Name,
		clitext.ParamReason: text.Text(mcpWeightCauseKey(result.Cause), nil),
	})
	return withServerStderr(text, line+"\n"+stopped, result.Stderr)
}

func withServerStderr(text *clitext.Translator, line, stderr string) string {
	if stderr == "" {
		return line
	}
	return line + "\n" + text.Text(clitext.ToolWeightStderr, clitext.Params{clitext.ParamStderr: strconv.Quote(stderr)})
}

func mcpWeightCauseKey(cause mcpweight.Cause) clitext.Key {
	switch cause {
	case mcpweight.CauseNotStdio:
		return clitext.ToolWeightCauseNotStdio
	case mcpweight.CauseNotStarted:
		return clitext.ToolWeightCauseNotStarted
	case mcpweight.CauseStart:
		return clitext.ToolWeightCauseStart
	case mcpweight.CauseExited:
		return clitext.ToolWeightCauseExited
	case mcpweight.CauseTimeout:
		return clitext.ToolWeightCauseTimeout
	case mcpweight.CauseBudget:
		return clitext.ToolWeightCauseBudget
	case mcpweight.CauseCanceled:
		return clitext.ToolWeightCauseCanceled
	case mcpweight.CauseRejected:
		return clitext.ToolWeightCauseRejected
	case mcpweight.CauseTooLarge:
		return clitext.ToolWeightCauseTooLarge
	case mcpweight.CauseTooManyPages:
		return clitext.ToolWeightCauseTooMany
	case mcpweight.CauseStop:
		return clitext.ToolWeightCauseStop
	case mcpweight.CauseProtocol:
		return clitext.ToolWeightCauseProtocol
	}
	return clitext.ToolWeightCauseProtocol
}
