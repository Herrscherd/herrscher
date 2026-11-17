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

func warnBounded(msg string, args ...any) {
	budget, stop := writeBudget()
	defer stop()
	logged := make(chan struct{})
	go func() {
		defer close(logged)
		slog.Warn(msg, args...)
	}()
	select {
	case <-logged:
	case <-budget:
	}
}

func warningWriter(writer io.Writer) io.Writer {
	if writer == nil {
		return os.Stderr
	}
	return writer
}

func warnTo(w io.Writer, lang i18n.Language, key string, params map[string]any) {
	logWriteFailure(fmt.Fprintln(warningWriter(w), newHostMessages().text(lang, key, params)))
}

func warnLive(w io.Writer, lang languageSource, key string, params map[string]any) {
	warnTo(w, lang(), key, params)
}
