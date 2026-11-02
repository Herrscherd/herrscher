package bridge

import "github.com/Herrscherd/herrscher/core/internal/obs"

var logger = obs.Stderr(false).With("component", "bridge")
