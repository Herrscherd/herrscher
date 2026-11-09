package host

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"

	"github.com/Herrscherd/herrscher/core/historywire"
)

func TestCompactionNotesNameTheStageThatFailed(t *testing.T) {
	failedJev := stageStats("jev", contracts.CompactFailed)
	doneBackend := stageStats("backend", contracts.CompactDone)
	failedBackend := stageStats("backend", contracts.CompactFailed)
	tests := []struct {
		name    string
		jev     statsCompactor
		backend statsCompactor
		wantErr bool
		want    map[string]string
	}{
		{
			name:    "a jev fallback without error keeps its own note, the backend ok",
			jev:     statsCompactor{stats: &failedJev},
			backend: statsCompactor{stats: &doneBackend},
			want:    map[string]string{"jev": fellBackNote},
		},
		{
			name:    "each failed stage keeps its own note when the backend fails too",
			jev:     statsCompactor{stats: &failedJev},
			backend: statsCompactor{stats: &failedBackend, err: errors.New("boom")},
			wantErr: true,
			want:    map[string]string{"jev": fellBackNote, "backend": unknownFailureNote},
		},
		{
			name:    "a known error is recorded as its class",
			jev:     statsCompactor{stats: &failedJev, err: errBridgeGone},
			backend: statsCompactor{stats: &doneBackend},
			wantErr: true,
			want:    map[string]string{"jev": "compaction.bridge_gone"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := runRecordedStack(t, tc.jev, tc.backend)
			if (err != nil) != tc.wantErr {
				t.Fatalf("RunCompactStack error = %v, want an error: %t", err, tc.wantErr)
			}
			if !maps.Equal(got, tc.want) {
				t.Fatalf("failed notes = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCompactionNoteNeverKeepsTheRawErrorText(t *testing.T) {
	failed := stageStats("jev", contracts.CompactFailed)
	raw := errors.New(`Get "https://api.example.test/v1/messages?api_key=XYZ": dial tcp: connection refused`)
	got, err := runRecordedStack(t, statsCompactor{stats: &failed, err: raw}, statsCompactor{})
	if err == nil {
		t.Fatal("RunCompactStack returned no error, want the stage failure")
	}
	note, ok := got["jev"]
	if !ok || note != unknownFailureNote {
		t.Fatalf("jev note = %q (recorded %t), want the class %q", note, ok, unknownFailureNote)
	}
	if strings.Contains(note, "XYZ") || strings.Contains(note, "api.example") {
		t.Fatalf("jev note = %q, want no part of the raw error", note)
	}
}

func TestEveryFailureClassHasAHealthText(t *testing.T) {
	errs := []error{errBridgeGone, errNoCompactorFactory, errNilCompactor, errNoCompactorPlugin, historywire.ErrNoHistoryEditor, errHistoryTimeout, historywire.ErrHistoryTooLarge, errors.New("unclassified")}
	for _, err := range errs {
		class := failureClass(err)
		if _, ok := compactNoteKey(class); !ok {
			t.Fatalf("failure class %q of %v has no health text", class, err)
		}
	}
	for _, class := range []string{fellBackNote, unknownFailureNote} {
		if _, ok := compactNoteKey(class); !ok {
			t.Fatalf("class %q has no health text", class)
		}
	}
}

func runRecordedStack(t *testing.T, jev, backend contracts.Compactor) (map[string]string, error) {
	t.Helper()
	notes := make(stageNotes)
	stages := []contracts.CompactStage{
		{Name: "jev", Compactor: recordedStage{name: "jev", compactor: jev, notes: notes}},
		{Name: "backend", Compactor: recordedStage{name: "backend", compactor: backend, notes: notes}},
	}
	result, err := contracts.RunCompactStack(context.Background(), stages, contracts.CompactRequest{}, nil)
	var capture usageCapture
	d := &sessionDriver{name: "s1", sink: sessionSink{Usage: capture.add}}
	d.recordCompaction(result.StageStats, notes)
	failed := make(map[string]string)
	records := capture.all()
	for index := range records {
		compaction := records[index].Compaction
		if compaction.Outcome == contracts.CompactFailed {
			failed[compaction.Stage] = compaction.Note
		}
	}
	return failed, err
}
