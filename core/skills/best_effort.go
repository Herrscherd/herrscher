package skills

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
)

func logFailure(action string, err error) {
	if err != nil {
		slog.Debug("skills best-effort step failed", "action", action, "err", err)
	}
}

func removeLogged(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		logFailure("remove "+path, err)
	}
}
