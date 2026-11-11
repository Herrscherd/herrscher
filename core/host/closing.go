package host

import (
	"fmt"
	"io"
	"log/slog"
	"os"

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

func warnHost(lang i18n.Language, key string, params map[string]any) {
	warnTo(os.Stderr, lang, key, params)
}

func warnTo(w io.Writer, lang i18n.Language, key string, params map[string]any) {
	logWriteFailure(fmt.Fprintln(w, newHostMessages().text(lang, key, params)))
}

func warnLive(lang languageSource, key string, params map[string]any) {
	warnHost(lang(), key, params)
}
