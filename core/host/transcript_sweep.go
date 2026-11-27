package host

import (
	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func sweepTranscriptStaging(partDir string, sessions []state.Session) {
	for idx := range sessions {
		logCleanup(atomicfile.Sweep(state.TranscriptPath(partDir, sessions[idx].Name)))
	}
}
