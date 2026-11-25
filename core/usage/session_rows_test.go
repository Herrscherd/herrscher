package usage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"

	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/termpaint"
)

func sessionRecord(session string, cost float64, in, read, create int64) Record {
	return Record{Time: testNow().Add(-time.Hour), Session: session, Vendor: "claude", Model: "opus", In: in, CacheRead: read, CacheCreate: create, Cost: cost}
}

func TestSummarizeGivesEachSessionItsCacheReadShare(t *testing.T) {
	records := []Record{
		sessionRecord("alpha", 2, 100, 300, 100),
		sessionRecord("alpha", 1, 100, 100, 0),
		sessionRecord("beta", 1, 50, 0, 0),
	}
	rows := Summarize(records, Range7d, testNow(), AccountKey{}, nil).Rows
	if len(rows) != 2 || rows[0].Session != "alpha" || rows[1].Session != "beta" {
		t.Fatalf("rows = %+v, want alpha then beta by cost", rows)
	}
	wantAlpha := float64(400) / float64(200+400+100) * percentScale
	if !near(rows[0].CacheRatio, wantAlpha) {
		t.Errorf("alpha cache ratio = %v, want %v", rows[0].CacheRatio, wantAlpha)
	}
	if rows[1].CacheRatio != 0 {
		t.Errorf("beta cache ratio = %v, want 0", rows[1].CacheRatio)
	}
	if rows[0].Tokens.CacheRead != 400 || rows[0].Cost != 3 {
		t.Errorf("alpha row = %+v, want its cache reads and its summed cost", rows[0])
	}
}

func TestSummarizeLeavesCompactionRecordsOutOfSessionRows(t *testing.T) {
	compaction := sessionRecord("gamma", 9, 10, 10, 10)
	compaction.Compaction = &Compaction{Stage: "backend"}
	rows := Summarize([]Record{compaction}, Range7d, testNow(), AccountKey{}, nil).Rows
	if len(rows) != 0 {
		t.Fatalf("a compaction run gave a session row: %+v", rows)
	}
}

func TestInjectionAverageNeedsATurnThatCarriesSizes(t *testing.T) {
	if _, ok := (Injection{}).Average(); ok {
		t.Fatal("a session with no sized turn has an average")
	}
	average, ok := Injection{Turns: 4, Tokens: 1000}.Average()
	if !ok || average != 250 {
		t.Fatalf("average = %v, %t, want 250, true", average, ok)
	}
}

func TestSummarizeAttachesEachSessionInjection(t *testing.T) {
	records := []Record{sessionRecord("alpha", 1, 10, 0, 0), sessionRecord("beta", 1, 10, 0, 0)}
	injected := map[string]Injection{"alpha": {Turns: 2, Tokens: 500}}
	rows := Summarize(records, Range7d, testNow(), AccountKey{}, injected).Rows
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want two sessions", rows)
	}
	byName := map[string]SessionRow{rows[0].Session: rows[0], rows[1].Session: rows[1]}
	if byName["alpha"].Injection != (Injection{Turns: 2, Tokens: 500}) {
		t.Errorf("alpha injection = %+v, want two turns of 500 tokens", byName["alpha"].Injection)
	}
	if _, ok := byName["beta"].Injection.Average(); ok {
		t.Errorf("beta has no sized turn but reads an average: %+v", byName["beta"].Injection)
	}
}

func TestSessionRowsJSONCarriesTheCacheShareAndTheInjection(t *testing.T) {
	snapshot := Snapshot{Range: Range7d, Spend: Spend{Rows: []SessionRow{{Session: "alpha", Cost: 2, CacheRatio: 60, Injection: Injection{Turns: 2, Tokens: 500}}}}}
	raw, err := Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Spend struct {
			Rows []struct {
				Session    string  `json:"session"`
				CacheRatio float64 `json:"cacheRatio"`
				Injection  struct {
					Turns  int   `json:"turns"`
					Tokens int64 `json:"tokens"`
				} `json:"injection"`
			} `json:"sessionRows"`
		} `json:"spend"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Spend.Rows) != 1 {
		t.Fatalf("sessionRows = %s, want one row", raw)
	}
	row := wire.Spend.Rows[0]
	if row.Session != "alpha" || row.CacheRatio != 60 || row.Injection.Turns != 2 || row.Injection.Tokens != 500 {
		t.Fatalf("session row = %+v in %s", row, raw)
	}
}

func TestTokenLinesShowTheInjectedEstimatePerSession(t *testing.T) {
	snapshot := Snapshot{Spend: Spend{
		Tokens: Tokens{In: 1000, Out: 100},
		Rows: []SessionRow{
			{Session: "alpha", Tokens: Tokens{In: 1000, Out: 100}, Injection: Injection{Turns: 2, Tokens: 500}},
			{Session: "beta", Tokens: Tokens{In: 10}},
		},
	}}
	lines, err := TokenLines(snapshot, clitext.NewMessages(), i18n.French, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"~250", "n/a", "estimation à environ quatre octets par jeton", "INJECTÉ PAR TOUR"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tokens miss %q:\n%s", want, joined)
		}
	}
}

func TestTokenLinesOmitTheEstimateNoteWhenNoTurnCarriesSizes(t *testing.T) {
	snapshot := Snapshot{Spend: Spend{
		Tokens: Tokens{In: 10},
		Rows:   []SessionRow{{Session: "beta", Tokens: Tokens{In: 10}}},
	}}
	lines, err := TokenLines(snapshot, clitext.NewMessages(), i18n.French, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "n/a") || strings.Contains(joined, "octets") {
		t.Fatalf("a session without sizes must read n/a with no estimate note:\n%s", joined)
	}
}

func TestCostLinesShowTheCacheReadShareOfEachSession(t *testing.T) {
	snapshot := Snapshot{Range: Range7d, Spend: Spend{Cost: 3, Sessions: 2, Rows: []SessionRow{
		{Session: "alpha", Cost: 2, CacheRatio: 60},
		{Session: "beta", Cost: 1, CacheRatio: 0},
	}}}
	lines, err := CostLines(snapshot, clitext.NewMessages(), i18n.French, time.UTC, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"LECTURE CACHE", "alpha", "60 %"} {
		if !strings.Contains(joined, want) {
			t.Errorf("cost misses %q:\n%s", want, joined)
		}
	}
}

func TestSessionTablesShowTheFirstRowsAndCountTheRest(t *testing.T) {
	var rows []SessionRow
	for index := range sessionCount {
		rows = append(rows, SessionRow{Session: "s" + string(rune('a'+index)), Cost: float64(sessionCount - index)})
	}
	snapshot := Snapshot{Range: Range7d, Spend: Spend{Cost: 1, Sessions: sessionCount, Tokens: Tokens{In: 1}, Rows: rows}}
	lines, err := CostLines(snapshot, clitext.NewMessages(), i18n.French, time.UTC, termpaint.Palette{})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "et 2 autres sessions") {
		t.Errorf("cost does not count the sessions it leaves out:\n%s", joined)
	}
	shown := string(rune('a' + shownSessionRows - 1))
	hidden := string(rune('a' + sessionCount - 1))
	if !strings.Contains(joined, "s"+shown) || strings.Contains(joined, "s"+hidden) {
		t.Errorf("cost shows the wrong rows:\n%s", joined)
	}
}

const sessionCount = shownSessionRows + 2
