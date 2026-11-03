package skills

import (
	"log/slog"
)

func logFailure(action string, err error) {
	if err != nil {
		slog.Debug("skills best-effort step failed", "action", action, "err", err)
	}
}
