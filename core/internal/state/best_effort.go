package state

import (
	"io"
	"log/slog"
)

func logFailure(action string, err error) {
	if err != nil {
		slog.Debug("state best-effort step failed", "action", action, "err", err)
	}
}

func closeLogged(closer io.Closer) {
	logFailure("close", closer.Close())
}
