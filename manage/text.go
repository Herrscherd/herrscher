package manage

import (
	"log/slog"

	"github.com/Herrscherd/herrscher/core/clitext"
)

func msg(key clitext.Key, params clitext.Params) string {
	translator := clitext.For(clitext.ClientLanguage())
	text := translator.Text(key, params)
	logRenderErr(translator)
	return text
}

func logRenderErr(translator *clitext.Translator) {
	if err := translator.Err(); err != nil {
		slog.Error("render message", "err", err)
	}
}

func failure(cause error, key clitext.Key, params clitext.Params) error {
	return clitext.Failure(clitext.ClientLanguage(), cause, key, params)
}

func stateWord(sub string) string {
	if sub == verbAdd {
		return msg(clitext.ManageStateAdded, nil)
	}
	return msg(clitext.ManageStateRemoved, nil)
}

func changeLine(sub, module, path string) string {
	if sub == verbAdd {
		return msg(clitext.ManagePluginAdded, clitext.Params{clitext.ParamModule: module, clitext.ParamPath: path})
	}
	return msg(clitext.ManagePluginRemoved, clitext.Params{clitext.ParamModule: module, clitext.ParamPath: path})
}
