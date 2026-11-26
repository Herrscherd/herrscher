package host

import (
	"context"
	"encoding/json"
	"io/fs"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/internal/mcpweight"
)

const paramMCPDeep = "deep"

type mcpWeightRequest struct {
	agent  string
	deep   bool
	asJSON bool
}

func mcpWeightVerb(cliMessages *clitext.Messages, lang languageSource, agents agentLookup) contracts.Cmd {
	return contracts.New("mcp", "weight").
		Help(clitext.CmdHelpMCPWeight).
		ValueParam(paramAgent, clitext.CmdParamMCPWeightAgent, true).
		Param(paramMCPDeep, clitext.CmdParamMCPWeightDeep, false).
		Do(func(ctx context.Context, in contracts.Input) (contracts.Result, error) {
			req := mcpWeightRequest{agent: in.Get(paramAgent), deep: in.Bool(paramMCPDeep), asJSON: in.JSON}
			return cli.Data(mcpWeightReport(ctx, cliMessages, lang(), agents, req))
		})
}

func mcpWeightReport(ctx context.Context, cliMessages *clitext.Messages, lang i18n.Language, agents agentLookup, req mcpWeightRequest) (string, error) {
	found, ok := agents.Get(req.agent)
	if !ok {
		return "", cliMessages.Failure(lang, fs.ErrNotExist, clitext.MCPWeightUnknownAgent, clitext.Params{clitext.ParamAgent: req.agent})
	}
	servers, err := mcpweight.ReadServers(found.MCPConfigPath())
	if err != nil {
		return "", err
	}
	results := make([]mcpweight.Result, 0, len(servers))
	for _, server := range servers {
		results = append(results, mcpWeigh(ctx, server, req.deep))
	}
	if req.asJSON {
		body, encodeErr := json.Marshal(results)
		return string(body), encodeErr
	}
	return renderMCPWeight(cliMessages, lang, req.agent, results), nil
}

func mcpWeigh(ctx context.Context, server mcpweight.Server, deep bool) mcpweight.Result {
	if !deep {
		return mcpweight.Describe(server)
	}
	return mcpweight.Measure(ctx, server, mcpweight.DefaultTimeout)
}

func renderMCPWeight(cliMessages *clitext.Messages, lang i18n.Language, agent string, results []mcpweight.Result) string {
	text := replyText(cliMessages, lang)
	if len(results) == 0 {
		return text.Text(clitext.MCPWeightNone, nil)
	}
	var body strings.Builder
	_, _ = body.WriteString(text.Text(clitext.MCPWeightHeader, clitext.Params{clitext.ParamAgent: agent}) + "\n")
	for _, result := range results {
		_, _ = body.WriteString(mcpWeightLine(text, result) + "\n")
	}
	_, _ = body.WriteString(text.Text(clitext.MCPWeightEstimate, nil))
	return body.String()
}

func mcpWeightLine(text *clitext.Translator, result mcpweight.Result) string {
	if result.Measured {
		return text.Text(clitext.MCPWeightMeasured, clitext.Params{
			clitext.ParamName:      result.Name,
			clitext.ParamTransport: result.Transport,
			clitext.ParamTools:     result.Tools,
			clitext.ParamBytes:     result.Bytes,
			clitext.ParamTokens:    result.Tokens,
		})
	}
	params := clitext.Params{
		clitext.ParamName:      result.Name,
		clitext.ParamTransport: result.Transport,
		clitext.ParamReason:    text.Text(mcpWeightCauseKey(result.Cause), nil),
	}
	if result.Detail == "" {
		return text.Text(clitext.MCPWeightFailed, params)
	}
	params[clitext.ParamDetail] = result.Detail
	return text.Text(clitext.MCPWeightFailedDetail, params)
}

func mcpWeightCauseKey(cause mcpweight.Cause) clitext.Key {
	switch cause {
	case mcpweight.CauseNotStdio:
		return clitext.MCPWeightCauseNotStdio
	case mcpweight.CauseNotStarted:
		return clitext.MCPWeightCauseNotStarted
	case mcpweight.CauseStart:
		return clitext.MCPWeightCauseStart
	case mcpweight.CauseExited:
		return clitext.MCPWeightCauseExited
	case mcpweight.CauseTimeout:
		return clitext.MCPWeightCauseTimeout
	case mcpweight.CauseRejected:
		return clitext.MCPWeightCauseRejected
	case mcpweight.CauseTooLarge:
		return clitext.MCPWeightCauseTooLarge
	case mcpweight.CauseTooManyPages:
		return clitext.MCPWeightCauseTooMany
	case mcpweight.CauseStop:
		return clitext.MCPWeightCauseStop
	case mcpweight.CauseProtocol:
		return clitext.MCPWeightCauseProtocol
	}
	return clitext.MCPWeightCauseProtocol
}
