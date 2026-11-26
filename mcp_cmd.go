package main

import (
	"context"
	"errors"
	"os"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/envx"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/selftools"
	"github.com/Herrscherd/herrscher/manage"
)

const (
	mcpVerb              = "mcp"
	selfToolsScope       = "SELF_TOOLS_EXPOSE"
	selfToolsCallTimeout = host.SelfToolsCallWait
)

var errNoDaemon = errors.New("herrscher: no daemon answers on the command socket of this session")

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
	lang := clitext.ClientLanguage()
	if len(args) > 0 {
		return renderError(messages, lang, errUnknownArgument, clitext.FlagUnknown, clitext.Params{clitext.ParamCommand: mcpVerb, clitext.ParamName: args[0]})
	}
	exposure, err := selftools.ParseExposure(envx.Get(selfToolsScope))
	if err != nil {
		return err
	}
	server := selftools.NewServer(sessionDaemon{layout: layout, messages: messages, hostMessages: hostMessages}, herrscherVersion(), exposure, setupCheckTool(messages, hostMessages))
	return server.Serve(ctx, os.Stdin, os.Stdout)
}

func setupCheckTool(messages *clitext.Messages, hostMessages *host.Messages) selftools.LocalTool {
	return selftools.LocalTool{
		Tool: selftools.BooleanTool("setup_check",
			"Report each section of the herrscher setup (connections, modules, gateways, backends, memory, routing) as ok, warning, bad or off, "+
				"the way `herrscher setup` shows it, without changing anything.",
			map[string]string{"probe": "Also check each account against its service. No sign-in is opened."}),
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			return manage.CheckSetup(ctx, messages, hostMessages, selftools.Flag(args, "probe")).JSON()
		},
	}
}
