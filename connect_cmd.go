package main

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/forgecreds"
	"github.com/Herrscherd/herrscher/core/host"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

const (
	connectVerb            = "connect"
	connectProviderGitHub  = "github"
	connectSubStatus       = "status"
	connectSubLogout       = "logout"
	connectArgHost         = "--host"
	connectFlagName        = "host"
	connectDefaultHost     = "github.com"
	connectGitHubHostEnv   = "GH_HOST"
	gitHubAppInstallURL    = "https://github.com/apps/herrscher-for-github/installations/new"
	gitHubAppRevokeURLBase = "https://github.com/settings/connections/applications/"
	connectExpiryLayout    = time.RFC3339
)

var errConnectUsage = errors.New("connect usage")

type connectOptions struct {
	host contracts.ForgeHost
}

func runConnect(ctx context.Context, messages *clitext.Messages, args []string) error {
	lang := clitext.ClientLanguage()
	if len(args) == 0 {
		return connectUsage(messages, lang)
	}
	creds := forgecreds.New(host.DefaultStatePath())
	switch args[0] {
	case connectProviderGitHub:
		opts, err := parseConnectArgs(messages, lang, args[1:])
		if err != nil {
			return err
		}
		return connectGitHub(ctx, messages, lang, creds, opts.host, os.Stdout)
	case connectSubStatus:
		if len(args) != 1 {
			return connectUsage(messages, lang)
		}
		return connectStatus(ctx, messages, lang, creds, os.Stdout)
	case connectSubLogout:
		opts, err := parseConnectArgs(messages, lang, args[1:])
		if err != nil {
			return err
		}
		return connectLogout(messages, lang, creds, opts.host, os.Stdout)
	}
	return connectUsage(messages, lang)
}

func connectUsage(messages *clitext.Messages, lang i18n.Language) error {
	text, err := messages.Text(lang, clitext.ConnectUsage, nil)
	if err != nil {
		return err
	}
	if err := writeCLIText(os.Stderr, text); err != nil {
		return err
	}
	return exitStatusError{code: exitUsage}
}

func defaultConnectOptions() (connectOptions, error) {
	parsed, err := contracts.ParseForgeHost(connectDefaultHost)
	return connectOptions{host: parsed}, err
}

func parseConnectArgs(messages *clitext.Messages, lang i18n.Language, args []string) (connectOptions, error) {
	opts, err := defaultConnectOptions()
	if err != nil {
		return opts, err
	}
	for index := 0; index < len(args); index++ {
		name, value, inline := strings.Cut(args[index], "=")
		if name != connectArgHost {
			return opts, connectUsage(messages, lang)
		}
		if !inline {
			if index+1 >= len(args) {
				return opts, renderError(messages, lang, errConnectUsage, clitext.FlagNeedsValue, clitext.Params{clitext.ParamCommand: connectVerb, clitext.ParamName: connectFlagName})
			}
			value = args[index+1]
			index++
		}
		parsed, parseErr := contracts.ParseForgeHost(value)
		if parseErr != nil {
			return opts, renderError(messages, lang, parseErr, clitext.ConnectHostInvalid, clitext.Params{clitext.ParamErr: termpaint.Escape(parseErr.Error()).String()})
		}
		opts.host = parsed
	}
	return opts, nil
}

func connectGitHub(ctx context.Context, messages *clitext.Messages, lang i18n.Language, creds *forgecreds.Credentials, forgeHost contracts.ForgeHost, output io.Writer) error {
	translator := messages.For(lang)
	err := creds.Connect(ctx, forgeHost, func(code forgecreds.DeviceCode) error {
		return writeText(output, translator, clitext.ConnectStart, clitext.Params{
			clitext.ParamHost:     forgeHost.String(),
			clitext.ParamURL:      code.VerificationURI,
			clitext.ParamUserCode: code.UserCode,
		})
	})
	if err != nil {
		return connectFailure(messages, lang, forgeHost, err)
	}
	return writeText(output, translator, clitext.ConnectDone, clitext.Params{clitext.ParamHost: forgeHost.String()})
}

func connectFailure(messages *clitext.Messages, lang i18n.Language, forgeHost contracts.ForgeHost, cause error) error {
	params := clitext.Params{clitext.ParamHost: forgeHost.String()}
	switch {
	case errors.Is(cause, forgecreds.ErrAppUnavailable):
		return renderError(messages, lang, cause, clitext.ConnectAppUnavailable, params)
	case errors.Is(cause, forgecreds.ErrDeviceExpired):
		return renderError(messages, lang, cause, clitext.ConnectDeviceExpired, nil)
	case errors.Is(cause, forgecreds.ErrDeviceDenied):
		return renderError(messages, lang, cause, clitext.ConnectDeviceDenied, nil)
	}
	params[clitext.ParamErr] = termpaint.Escape(cause.Error()).String()
	return renderError(messages, lang, cause, clitext.ConnectFailed, params)
}

func connectStatus(ctx context.Context, messages *clitext.Messages, lang i18n.Language, creds *forgecreds.Credentials, output io.Writer) error {
	translator := messages.For(lang)
	hosts, err := statusHosts(creds)
	if err != nil {
		return renderError(messages, lang, err, clitext.ConnectFailed, clitext.Params{clitext.ParamHost: connectDefaultHost, clitext.ParamErr: termpaint.Escape(err.Error()).String()})
	}
	for _, forgeHost := range hosts {
		line, err := statusLine(ctx, messages, lang, creds, forgeHost)
		if err != nil {
			return err
		}
		if err := emitf(output, "%s\n", line); err != nil {
			return err
		}
	}
	if err := writeText(output, translator, clitext.ConnectInstallLine, clitext.Params{clitext.ParamURL: gitHubAppInstallURL}); err != nil {
		return err
	}
	return writeText(output, translator, clitext.ConnectRevokeLine, clitext.Params{clitext.ParamURL: gitHubAppRevokeURLBase + forgecreds.ClientID})
}

func statusHosts(creds *forgecreds.Credentials) ([]contracts.ForgeHost, error) {
	saved, err := creds.Store().Hosts()
	if err != nil {
		return nil, err
	}
	names := []string{connectDefaultHost}
	slices.Sort(saved)
	names = append(names, saved...)
	if named := strings.TrimSpace(os.Getenv(connectGitHubHostEnv)); named != "" {
		names = append(names, named)
	}
	hosts := make([]contracts.ForgeHost, 0, len(names))
	for _, name := range names {
		parsed, err := contracts.ParseForgeHost(name)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(hosts, parsed) {
			hosts = append(hosts, parsed)
		}
	}
	return hosts, nil
}

func statusLine(ctx context.Context, messages *clitext.Messages, lang i18n.Language, creds *forgecreds.Credentials, forgeHost contracts.ForgeHost) (string, error) {
	translator := messages.For(lang)
	access, err := accessText(messages, lang, creds, forgeHost)
	if err != nil {
		return "", err
	}
	source := translator.Text(clitext.ConnectSourceNone, nil)
	login := translator.Text(clitext.ConnectLoginUnknown, nil)
	if token, tokenErr := creds.Token(ctx, forgeHost); tokenErr == nil {
		source = token.Source().String()
		login = connectionLogin(ctx, creds, forgeHost, translator)
	}
	if err := translator.Err(); err != nil {
		return "", err
	}
	return translator.Render(clitext.ConnectStatusLine, clitext.Printable(clitext.Params{
		clitext.ParamHost:   forgeHost.String(),
		clitext.ParamSource: source,
		clitext.ParamLogin:  login,
		clitext.ParamAccess: access,
	}))
}

func connectionLogin(ctx context.Context, creds *forgecreds.Credentials, forgeHost contracts.ForgeHost, translator *clitext.Translator) string {
	unknown := translator.Text(clitext.ConnectLoginUnknown, nil)
	forges := contracts.Default.Forges()
	if len(forges) == 0 || forges[0].Forge == nil {
		return unknown
	}
	forge, err := forges[0].Forge(ctx, contracts.PluginConfig{}, forgeHost, creds)
	if err != nil {
		return unknown
	}
	connection, err := forge.Connection(ctx)
	if err != nil || connection.Login == "" {
		return unknown
	}
	return connection.Login
}

func accessText(messages *clitext.Messages, lang i18n.Language, creds *forgecreds.Credentials, forgeHost contracts.ForgeHost) (string, error) {
	translator := messages.For(lang)
	record, found, err := creds.Store().Get(forgeHost)
	if err != nil {
		return "", renderError(messages, lang, err, clitext.ConnectFailed, clitext.Params{clitext.ParamHost: forgeHost.String(), clitext.ParamErr: termpaint.Escape(err.Error()).String()})
	}
	switch {
	case found && record.Rejected:
		return translator.Text(clitext.ConnectAccessRejected, nil), nil
	case found && !record.AccessExpiresAt.IsZero():
		return translator.Text(clitext.ConnectAccessExpires, clitext.Params{clitext.ParamTime: record.AccessExpiresAt.UTC().Format(connectExpiryLayout)}), nil
	}
	return translator.Text(clitext.ConnectAccessNever, nil), nil
}

func connectLogout(messages *clitext.Messages, lang i18n.Language, creds *forgecreds.Credentials, forgeHost contracts.ForgeHost, output io.Writer) error {
	removed, err := creds.Store().Delete(forgeHost)
	if err != nil {
		return renderError(messages, lang, err, clitext.ConnectFailed, clitext.Params{clitext.ParamHost: forgeHost.String(), clitext.ParamErr: termpaint.Escape(err.Error()).String()})
	}
	params := clitext.Params{clitext.ParamHost: forgeHost.String()}
	translator := messages.For(lang)
	if removed {
		return writeText(output, translator, clitext.ConnectLogoutDone, params)
	}
	return writeText(output, translator, clitext.ConnectLogoutNothing, params)
}
