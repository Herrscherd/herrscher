package host

import (
	"log/slog"

	"github.com/Herrscherd/herrscher/core/internal/obs"
)

// Logger builds the operator logger for composition roots outside core (the root
// `main` package can't reach core/internal/obs directly). The level follows the
// -v flag and HERRSCHER_LOG, matching the daemon's own logger.
func Logger(verbose bool) *slog.Logger {
	return obs.Stderr(verbose)
}
