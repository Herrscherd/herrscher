package host

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func closeRead(closer io.Closer) {
	logCleanup(closer.Close())
}

func logCleanup(err error) {
	if err != nil {
		slog.Debug("cleanup failed", "err", err)
	}
}

func logWriteFailure(_ int, err error) {
	logCleanup(err)
}

func warnTo(w io.Writer, lang i18n.Language, key string, params map[string]any) {
	logWriteFailure(fmt.Fprintln(w, newHostMessages().text(lang, key, params)))
}

func warnLive(w io.Writer, lang languageSource, key string, params map[string]any) {
	warnTo(w, lang(), key, params)
}
