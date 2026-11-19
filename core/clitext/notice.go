package clitext

import (
	"log/slog"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
)

// RenderNotice renders a contracts notice in lang, or returns "" when it cannot be rendered.
func RenderNotice(lang i18n.Language, notice contracts.Notice) string {
	text, err := contracts.NewRenderer().Render(lang, notice)
	if err != nil {
		slog.Warn("notice not rendered, the line is left without text", "code", notice.Code, "err", err)
		return ""
	}
	return text
}
