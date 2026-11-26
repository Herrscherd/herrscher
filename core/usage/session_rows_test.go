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
	rows := Summarize(records, Range7d, testNow(), AccountKey{}).Rows
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
	rows := Summarize([]Record{compaction}, Range7d, testNow(), AccountKey{}).Rows
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

func measuredRecord(session string, tokens int64) Record {
	record := sessionRecord(session, 1, 10, 0, 0)
	record.InjectedMeasured = true
	record.InjectedTokens = tokens
	return record
}

func rowsByName(rows []SessionRow) map[string]SessionRow {
	byName := make(map[string]SessionRow, len(rows))
	for _, row := range rows {
		byName[row.Session] = row
	}
	return byName
}

func TestSummarizeAveragesOnlyTheMeasuredTurnsOfEachSession(t *testing.T) {
	records := []Record{
		measuredRecord("alpha", 500),
		measuredRecord("alpha", 300),
		sessionRecord("alpha", 1, 10, 0, 0),
		sessionRecord("beta", 1, 10, 0, 0),
	}
	byName := rowsByName(Summarize(records, Range7d, testNow(), AccountKey{}).Rows)
	if byName["alpha"].Injection != (Injection{Turns: 2, Tokens: 800}) {
		t.Errorf("alpha injection = %+v, want two measured turns of 800 tokens", byName["alpha"].Injection)
	}
	if _, ok := byName["beta"].Injection.Average(); ok {
		t.Errorf("beta has no measured turn but reads an average: %+v", byName["beta"].Injection)
	}
}

func TestSummarizeCountsAMeasuredTurnOfZeroTokens(t *testing.T) {
	rows := Summarize([]Record{measuredRecord("alpha", 0)}, Range7d, testNow(), AccountKey{}).Rows
	average, ok := rows[0].Injection.Average()
	if !ok || average != 0 {
		t.Fatalf("average = %v, %t, want 0, true for a measured turn of zero tokens", average, ok)
	}
}

func TestSummarizeTakesEachSessionInjectionFromItsOwnRecords(t *testing.T) {
	records := []Record{measuredRecord("old-name", 400), sessionRecord("new-name", 3, 10, 0, 0)}
	byName := rowsByName(Summarize(records, Range7d, testNow(), AccountKey{}).Rows)
	if byName["old-name"].Injection != (Injection{Turns: 1, Tokens: 400}) {
		t.Errorf("old-name injection = %+v, want its one measured turn", byName["old-name"].Injection)
	}
	if _, ok := byName["new-name"].Injection.Average(); ok {
		t.Errorf("new-name has no measured turn but reads an average: %+v", byName["new-name"].Injection)
	}
	if byName["new-name"].Cost != 3 {
		t.Errorf("new-name cost = %v, want the cost of its own records", byName["new-name"].Cost)
	}
}

func TestRecordWithoutInjectionFieldReadsAsUnmeasured(t *testing.T) {
	var record Record
	if err := json.Unmarshal([]byte(`{"session":"alpha","cost":1}`), &record); err != nil {
		t.Fatal(err)
	}
	if record.InjectedMeasured {
		t.Fatal("an old record without the field reads as measured")
	}
	rows := Summarize([]Record{record}, Range7d, testNow(), AccountKey{}).Rows
	if _, ok := rows[0].Injection.Average(); ok {
		t.Errorf("an old record reads an average: %+v", rows[0].Injection)
	}
}

func TestUnmeasuredRecordOmitsTheInjectionFieldsOnDisk(t *testing.T) {
	raw, err := json.Marshal(sessionRecord("alpha", 1, 10, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "injected") {
		t.Fatalf("an unmeasured record writes injection fields: %s", raw)
	}
}

func TestSummarizeDropsSessionRowsWithoutAName(t *testing.T) {
	spend := Summarize([]Record{sessionRecord("", 4, 10, 0, 0), sessionRecord("alpha", 1, 10, 0, 0)}, Range7d, testNow(), AccountKey{})
	if len(spend.Rows) != 1 || spend.Rows[0].Session != "alpha" {
		t.Fatalf("rows = %+v, want only the named session alpha", spend.Rows)
	}
	if spend.Sessions != 1 {
		t.Errorf("sessions = %d, want 1 named session", spend.Sessions)
	}
	if spend.Cost != 5 {
		t.Errorf("cost = %v, want the unnamed record still counted in the totals", spend.Cost)
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
