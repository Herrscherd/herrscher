package manage

import (
	"log/slog"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func msg(messages *clitext.Messages, key clitext.Key, params clitext.Params) string {
	translator := messages.For(clitext.ClientLanguage())
	text := translator.Text(key, params)
	logRenderErr(translator)
	return text
}

func logRenderErr(translator *clitext.Translator) {
	if err := translator.Err(); err != nil {
		slog.Error("render message", "err", err)
	}
}

func failure(messages *clitext.Messages, cause error, key clitext.Key, params clitext.Params) error {
	return messages.Failure(clitext.ClientLanguage(), cause, key, params)
}

func stateWord(messages *clitext.Messages, sub string) string {
	if sub == verbAdd {
		return msg(messages, clitext.ManageStateAdded, nil)
	}
	return msg(messages, clitext.ManageStateRemoved, nil)
}

func changeLine(messages *clitext.Messages, sub, module, path string) string {
	if sub == verbAdd {
		return msg(messages, clitext.ManagePluginAdded, clitext.Params{clitext.ParamModule: module, clitext.ParamPath: path})
	}
	return msg(messages, clitext.ManagePluginRemoved, clitext.Params{clitext.ParamModule: module, clitext.ParamPath: path})
}
