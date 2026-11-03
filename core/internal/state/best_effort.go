package state

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
)

func logFailure(action string, err error) {
	if err != nil {
		slog.Debug("state best-effort step failed", "action", action, "err", err)
	}
}

func closeLogged(closer io.Closer) {
	logFailure("close", closer.Close())
}

func removeLogged(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logFailure("remove "+path, err)
	}
}
