package health_test

import (
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/health"
)

func TestCompactionFactsNameTheLastRunOrItsAbsence(t *testing.T) {
	last := time.Date(2026, 11, 8, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		name       string
		compaction *health.Compaction
		lang       i18n.Language
		want       string
	}{
		{name: "never run, english", lang: i18n.English, want: "no compaction in the last 7 days"},
		{name: "never run, french", lang: i18n.French, want: "aucune compaction sur les 7 derniers jours"},
		{
			name:       "last run failed, french",
			compaction: &health.Compaction{At: last, Outcome: contracts.CompactFailed},
			lang:       i18n.French,
			want:       "dernière compaction : échouée, " + last.Local().Format(time.DateTime),
		},
		{
			name:       "last run done, english",
			compaction: &health.Compaction{At: last, Outcome: contracts.CompactDone},
			lang:       i18n.English,
			want:       "last compaction: done, " + last.Local().Format(time.DateTime),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := health.Snapshot{Checks: []health.Check{
				{Group: health.GroupCompaction, Name: "jev", Facts: health.Facts{Compaction: tc.compaction}},
			}}
			rows, err := snapshot.Rows(snapshot.Checks, tc.lang)
			if err != nil {
				t.Fatalf("Rows: %v", err)
			}
			if len(rows) != 1 || !strings.Contains(rows[0].Facts, tc.want) {
				t.Fatalf("facts = %q, want them to contain %q", rows[0].Facts, tc.want)
			}
		})
	}
}

func TestCompactionFactsSurviveTheWire(t *testing.T) {
	at := time.Date(2026, 11, 8, 14, 30, 0, 0, time.UTC)
	snapshot := health.Snapshot{Checks: []health.Check{
		{Group: health.GroupCompaction, Name: "jev", Facts: health.Facts{Compaction: &health.Compaction{At: at, Outcome: contracts.CompactShort}}},
		{Group: health.GroupCompaction, Name: "backend"},
	}}
	data, err := health.Encode(snapshot)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	back, err := health.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(back.Checks) != 2 {
		t.Fatalf("checks = %+v, want two", back.Checks)
	}
	got := back.Checks[0].Facts.Compaction
	if got == nil || !got.At.Equal(at) || got.Outcome != contracts.CompactShort {
		t.Fatalf("compaction = %+v, want the short run of %v", got, at)
	}
	if back.Checks[1].Facts.Compaction != nil {
		t.Fatalf("compaction = %+v, want none for a stage that never ran", back.Checks[1].Facts.Compaction)
	}
}
