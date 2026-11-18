package host

import (
	"context"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func (d *sessionDriver) currentLanguage() i18n.Language {
	if d.language == nil {
		return 0
	}
	return d.language()
}

func (d *sessionDriver) textLanguage() languageSource {
	return func() i18n.Language {
		if lang := d.currentLanguage(); lang != 0 {
			return lang
		}
		if d.operator != nil {
			return d.operator()
		}
		return 0
	}
}

func (d *sessionDriver) announceLanguage(ctx context.Context) bool {
	if d.language == nil {
		return true
	}
	return d.sendToBridge(ctx, contracts.Event{T: eventwire.EventLanguage, Value: languageTag(d.language())})
}

func languageTag(lang i18n.Language) string {
	if lang == 0 {
		return ""
	}
	return lang.String()
}
