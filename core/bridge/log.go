package bridge

import "github.com/Herrscherd/herrscher/core/internal/obs"

// logger is the bridge's operator log. The bridge runs as a subprocess the
// daemon spawns with its own stderr, so a line written here lands in the
// operator's log next to the daemon's. It exists for failures that are real but
// not worth failing a turn over — a dropped memory write, a backend that errored
// after producing partial output — which would otherwise be invisible.
//
// Built once at init: the level comes from the environment, which does not
// change under a running process.
var logger = obs.Stderr(false).With("component", "bridge")
