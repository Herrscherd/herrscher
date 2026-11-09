package health

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
)

func TestCompactionFactsShowTheKeyTheLastReplyAndTheLastError(t *testing.T) {
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		facts Facts
		want  []string
	}{
		{
			name:  "key absent, reply and error with no note",
			facts: Facts{CompactionKey: &CompactionKey{Env: "TYPESAFE_API_KEY"}, Compaction: &Compaction{At: at, Outcome: contracts.CompactFailed}, CompactionSucceeded: &Compaction{At: at, Outcome: contracts.CompactDone}, CompactionFailed: &Compaction{At: at}},
			want:  []string{"key TYPESAFE_API_KEY absent", "last reply: done", "last error:", "no detail"},
		},
		{
			name:  "key present and the error note",
			facts: Facts{CompactionKey: &CompactionKey{Env: "TYPESAFE_API_KEY", Present: true}, CompactionFailed: &Compaction{At: at, Key: clitext.HealthNoteUnknown}},
			want:  []string{"key TYPESAFE_API_KEY present", "last error:", "no code of its own"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := Snapshot{Checks: []Check{{Group: GroupCompaction, Name: "jev", Facts: tc.facts}}}
			view, err := snapshot.View(i18n.English)
			if err != nil {
				t.Fatalf("View: %v", err)
			}
			facts := view.Sections[0].Rows[0].Facts
			for _, want := range tc.want {
				if !strings.Contains(facts, want) {
					t.Fatalf("facts = %q, want them to contain %q", facts, want)
				}
			}
		})
	}
}
