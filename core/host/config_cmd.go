package host

import (
	"context"
	"strings"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/config"
)

const (
	nsConfig   = "config"
	paramKey   = "key"
	paramValue = "value"
)

func configVerbs(cliMessages *clitext.Messages, lang languageSource, path func() string) []contracts.Cmd {
	return []contracts.Cmd{
		contracts.New(nsConfig, "get").
			Help(clitext.CmdHelpConfigGet).
			ValueParam(paramKey, clitext.CmdParamConfigGetKey, false).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				doc, err := config.ReadDocument(path())
				if err != nil {
					return contracts.Result{}, err
				}
				return cli.Data(doc.Get(firstOf(input.Get(paramKey), input.Rest)))
			}),
		contracts.New(nsConfig, "set").
			Help(clitext.CmdHelpConfigSet).
			Param(paramKey, clitext.CmdParamConfigSetKey, true).
			Param(paramValue, clitext.CmdParamConfigSetValue, true).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				return cli.Data(editConfig(cliMessages, lang, path(), input.Get(paramKey), clitext.HostConfigSet, func(doc config.Document, key string) error {
					return doc.Set(key, input.Get(paramValue))
				}))
			}),
		contracts.New(nsConfig, "unset").
			Help(clitext.CmdHelpConfigUnset).
			Param(paramKey, clitext.CmdParamConfigUnsetKey, true).
			Do(func(_ context.Context, input contracts.Input) (contracts.Result, error) {
				return cli.Data(editConfig(cliMessages, lang, path(), input.Get(paramKey), clitext.HostConfigUnset, config.Document.Unset))
			}),
	}
}

func editConfig(cliMessages *clitext.Messages, lang languageSource, path, rawKey string, done clitext.Key, edit func(config.Document, string) error) (string, error) {
	key := strings.TrimSpace(rawKey)
	doc, err := config.ReadDocument(path)
	if err != nil {
		return "", err
	}
	if editErr := edit(doc, key); editErr != nil {
		return "", editErr
	}
	backup, err := doc.Write(path)
	if err != nil {
		return "", err
	}
	text := replyText(cliMessages, lang())
	out := text.Text(done, clitext.Params{clitext.ParamKey: key, clitext.ParamPath: path})
	if backup != "" {
		out += "\n" + text.Text(clitext.HostConfigBackup, clitext.Params{clitext.ParamPath: backup})
	}
	return out, nil
}
