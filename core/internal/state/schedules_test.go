package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/schedule"
)

func TestSchedulesSurviveARoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	sc := schedule.Schedule{Name: "digest", Agent: "scout", Every: "24h", Task: "read the PRs", CreatedAt: "2026-08-25T09:00:00Z"}
	if err := s.PutSchedule(sc); err != nil {
		t.Fatalf("PutSchedule: %v", err)
	}
	back, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	got := back.SnapshotSchedules()
	if len(got) != 1 || got[0].Name != "digest" || got[0].Task != "read the PRs" {
		t.Fatalf("SnapshotSchedules = %+v, want the one written", got)
	}
}

func TestPutScheduleUpsertsByName(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := s.PutSchedule(schedule.Schedule{Name: "digest", Task: "old"}); err != nil {
		t.Fatalf("PutSchedule: %v", err)
	}
	if err := s.PutSchedule(schedule.Schedule{Name: "digest", Task: "new"}); err != nil {
		t.Fatalf("PutSchedule again: %v", err)
	}
	got := s.SnapshotSchedules()
	if len(got) != 1 || got[0].Task != "new" {
		t.Fatalf("SnapshotSchedules = %+v, want one row carrying the new task", got)
	}
}

func TestScheduleMutatorsReportAnUnknownName(t *testing.T) {
	s := NewState(filepath.Join(t.TempDir(), "state.json"))
	if ok, err := s.RemoveSchedule("nope"); ok || err != nil {
		t.Errorf("RemoveSchedule(unknown) = %v, %v, want false, nil", ok, err)
	}
	if ok, err := s.SetSchedulePaused("nope", true); ok || err != nil {
		t.Errorf("SetSchedulePaused(unknown) = %v, %v, want false, nil", ok, err)
	}
	// StampScheduleRun est le seul appele depuis la boucle de tir : il ne doit
	// pas se plaindre d'un horaire supprime entre le tick et le tampon.
	if err := s.StampScheduleRun("nope", "2026-08-25T09:00:00Z"); err != nil {
		t.Errorf("StampScheduleRun(unknown) = %v, want nil", err)
	}
}

func TestSchedulePauseAndStampPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := NewState(path)
	if err := s.PutSchedule(schedule.Schedule{Name: "digest", Task: "t", CreatedAt: "2026-08-25T09:00:00Z"}); err != nil {
		t.Fatalf("PutSchedule: %v", err)
	}
	if ok, err := s.SetSchedulePaused("digest", true); !ok || err != nil {
		t.Fatalf("SetSchedulePaused = %v, %v", ok, err)
	}
	if err := s.StampScheduleRun("digest", "2026-08-25T10:00:00Z"); err != nil {
		t.Fatalf("StampScheduleRun: %v", err)
	}
	back, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	got := back.SnapshotSchedules()
	if len(got) != 1 || !got[0].Paused || got[0].LastRun != "2026-08-25T10:00:00Z" {
		t.Fatalf("SnapshotSchedules = %+v, want it paused and stamped", got)
	}
	if ok, err := back.RemoveSchedule("digest"); !ok || err != nil {
		t.Fatalf("RemoveSchedule = %v, %v", ok, err)
	}
	if got := back.SnapshotSchedules(); len(got) != 0 {
		t.Fatalf("SnapshotSchedules = %+v, want nothing left", got)
	}
}

func TestAStateFileWithoutSchedulesLoadsNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"home":{},"sessions":[]}`), 0o600); err != nil {
		t.Fatalf("seed state.json: %v", err)
	}
	s, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got := s.SnapshotSchedules(); len(got) != 0 {
		t.Fatalf("SnapshotSchedules = %+v, want none", got)
	}
}
