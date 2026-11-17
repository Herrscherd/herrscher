package manager

import (
	"errors"
	"slices"
	"testing"
)

type recordingTracker struct {
	events []string
}

func (r *recordingTracker) Begin() func() {
	r.events = append(r.events, "begin")
	return func() { r.events = append(r.events, "end") }
}

func TestPersistedBracketsTheWriteWithBeginAndEnd(t *testing.T) {
	tracker := &recordingTracker{}
	h := &Handler{}
	h.SetWriteTracker(tracker)

	err := h.persisted(func() error {
		tracker.events = append(tracker.events, "write")
		return nil
	})
	if err != nil {
		t.Fatalf("persisted returned %v, want nil", err)
	}
	want := []string{"begin", "write", "end"}
	if !slices.Equal(tracker.events, want) {
		t.Fatalf("events = %v, want %v", tracker.events, want)
	}
}

func TestPersistedReturnsTheWriteErrorAndStillEnds(t *testing.T) {
	tracker := &recordingTracker{}
	h := &Handler{}
	h.SetWriteTracker(tracker)
	boom := errors.New("disk full")

	err := h.persisted(func() error { return boom })

	if !errors.Is(err, boom) {
		t.Fatalf("persisted returned %v, want %v", err, boom)
	}
	want := []string{"begin", "end"}
	if !slices.Equal(tracker.events, want) {
		t.Fatalf("events = %v, want %v", tracker.events, want)
	}
}

func TestPersistedWithoutATrackerRunsTheWrite(t *testing.T) {
	h := &Handler{}
	ran := false

	err := h.persisted(func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("persisted returned %v, want nil", err)
	}
	if !ran {
		t.Fatal("write did not run without a tracker")
	}
}
