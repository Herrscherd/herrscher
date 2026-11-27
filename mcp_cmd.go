package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/selftools"
)

const (
	mcpVerb              = "mcp"
	mcpWeightSubcommand  = "weight"
	selfToolsScope       = "SELF_TOOLS_EXPOSE"
	selfToolsCallTimeout = host.SelfToolsCallWait
)

var (
	errNoDaemon        = errors.New("herrscher: no daemon answers on the command socket of this session")
	errNoSessionSocket = errors.New("herrscher mcp: no session command socket (" + host.SessionCommandSocketVar + "): the self tools run inside a session, never on the operator's socket")
)

type sessionDaemon struct {
	layout       host.RuntimeLayout
	messages     *clitext.Messages
	hostMessages *host.Messages
}

func (d sessionDaemon) Dispatch(ctx context.Context, argv []string) (string, error) {
	out, handled, err := forwardWithin(ctx, selfToolsCallTimeout, d.layout, d.hostMessages, argv)
	if !handled {
		return "", errNoDaemon
	}
	if err != nil {
		return "", d.hostMessages.Localize(d.messages, clitext.ClientLanguage(), err)
	}
	return out, nil
}

func runMCP(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	if len(args) > 0 && args[0] == mcpWeightSubcommand {
		return runMCPWeight(ctx, layout, messages, hostMessages, args)
	}
	lang := clitext.ClientLanguage()
	if len(args) > 0 {
		return renderError(messages, lang, errUnknownArgument, clitext.FlagUnknown, clitext.Params{clitext.ParamCommand: mcpVerb, clitext.ParamName: args[0]})
	}
	if strings.TrimSpace(os.Getenv(host.SessionCommandSocketVar)) == "" {
		return errNoSessionSocket
	}
	exposure := chosenExposure()
	server := selftools.NewServer(sessionDaemon{layout: layout, messages: messages, hostMessages: hostMessages}, herrscherVersion(), exposure)
	return server.Serve(ctx, os.Stdin, os.Stdout)
}

func chosenExposure() selftools.Exposure {
	asked := envx.Get(selfToolsScope)
	exposure, err := selftools.ParseExposure(asked)
	if err != nil {
		slog.Warn("self tools: unknown exposure, serving the core set", "err", err)
	}
	return exposure
}
