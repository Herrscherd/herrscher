//go:build e2e

package host

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/state"
)

func TestScheduleResumeStampsTheInjectedInstant(t *testing.T) {
	t.Setenv(envSchedulerClock, "2030-01-01T09:00:00Z")
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	reg := testScheduleRegistry(t, st, nil)
	ctx := context.Background()
	for _, args := range [][]string{
		{"schedule", "add", "--name", "digest", "--agent", "scout", "--every", "30m", "--task", "t"},
		{"schedule", "pause", "--name", "digest"},
		{"schedule", "resume", "--name", "digest"},
	} {
		if _, err := reg.Dispatch(ctx, args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	got := st.SnapshotSchedules()
	if len(got) != 1 || got[0].LastRun != "2030-01-01T09:00:00Z" {
		t.Fatalf("SnapshotSchedules = %+v, want the resume stamped with the injected instant", got)
	}
}
