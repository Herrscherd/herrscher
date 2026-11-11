package host

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
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
	logWriteFailure(fmt.Fprintln(os.Stderr, newHostMessages().text(lang, key, params)))
}

func warnClient(key string, params map[string]any) {
	warnHost(clitext.ClientLanguage(), key, params)
}
