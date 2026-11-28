package host

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/testsupport/stagingtest"
)

func TestLoadHubStateSweepsTheStagingFilesOfTheState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	stale := filepath.Join(dir, ".state.json.4242580")
	fresh := filepath.Join(dir, ".state.json.4242581")
	stagingtest.Plant(t, stale, time.Hour)
	stagingtest.Plant(t, fresh, time.Second)
	transcripts := state.TranscriptsDir(dir)
	if err := os.MkdirAll(transcripts, 0o700); err != nil {
		t.Fatal(err)
	}
	ghostStale := filepath.Join(transcripts, ".ghost.jsonl.4242582")
	ghostFresh := filepath.Join(transcripts, ".ghost.jsonl.4242583")
	stagingtest.Plant(t, ghostStale, time.Hour)
	stagingtest.Plant(t, ghostFresh, time.Second)

	if _, _, err := loadHubState(t.Context(), newHostMessages(), discardAnnouncer(), Options{StatePath: statePath}); err != nil {
		t.Fatalf("loadHubState: %v", err)
	}

	stagingtest.Absent(t, stale)
	stagingtest.Present(t, fresh)
	stagingtest.Absent(t, ghostStale)
	stagingtest.Present(t, ghostFresh)
}

func TestSweepTranscriptStagingCoversSessionsAbsentFromState(t *testing.T) {
	partDir := t.TempDir()
	transcripts := state.TranscriptsDir(partDir)
	if err := os.MkdirAll(transcripts, 0o700); err != nil {
		t.Fatal(err)
	}
	ghostStale := filepath.Join(transcripts, ".ghost.jsonl.4242590")
	ghostFresh := filepath.Join(transcripts, ".ghost.jsonl.4242591")
	stagingtest.Plant(t, ghostStale, time.Hour)
	stagingtest.Plant(t, ghostFresh, time.Second)

	sweepTranscriptStaging(partDir)

	stagingtest.Absent(t, ghostStale)
	stagingtest.Present(t, ghostFresh)
}
