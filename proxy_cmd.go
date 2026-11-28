package main

import (
	"context"
	"os"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/host"
)

const proxyVerb = "proxy"

func runProxy(ctx context.Context, layout host.RuntimeLayout, messages *clitext.Messages, hostMessages *host.Messages, args []string) error {
	lang := clitext.ClientLanguage()
	if len(args) != 1 {
		return proxyUsage(messages, lang)
	}
	channel, err := host.ParseProxyChannel(args[0])
	if err != nil {
		return proxyUsage(messages, lang)
	}
	return hostMessages.Localize(messages, lang, host.RunProxy(ctx, layout, host.DefaultStatePath(), configuredInstanceID(), channel, os.Stdin, os.Stdout))
}

func proxyUsage(messages *clitext.Messages, lang i18n.Language) error {
	text, err := messages.Text(lang, clitext.ProxyUsage, nil)
	if err != nil {
		return err
	}
	if err := writeCLIText(os.Stderr, text); err != nil {
		return err
	}
	return exitStatusError{code: exitUsage}
}
