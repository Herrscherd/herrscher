package host

import (
	"github.com/Herrscherd/herrscher/core/internal/atomicfile"
	"github.com/Herrscherd/herrscher/core/internal/state"
)

func sweepTranscriptStaging(partDir string) {
	logCleanup(atomicfile.SweepDir(state.TranscriptsDir(partDir), ".jsonl"))
}
