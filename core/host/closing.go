package host

import (
	"fmt"
	"io"
	"log/slog"
	"os"
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

func warnStderr(format string, args ...any) {
	logWriteFailure(fmt.Fprintf(os.Stderr, format, args...))
}
