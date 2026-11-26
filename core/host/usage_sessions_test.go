package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/usage"
)

const vendorTranscriptSession = "vendor-uuid"

func sessionUsageSource(t *testing.T, now time.Time) *usageSource {
	t.Helper()
	return &usageSource{
		transcripts: &usage.TranscriptIndex{Path: filepath.Join(t.TempDir(), "transcripts.json"), Now: func() time.Time { return now }},
		prices:      &usage.PriceSource{Path: filepath.Join(t.TempDir(), "prices.json")},
	}
}

func coverClaudeTranscripts(t *testing.T, source *usageSource, now time.Time) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "projects", "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"sessionId":%q,"requestId":"r1","message":{"id":"m1","model":"claude-opus-4-1","usage":{"input_tokens":10,"cache_read_input_tokens":100,"cache_creation_input_tokens":20,"output_tokens":5}}}`, now.UTC().Format(time.RFC3339), vendorTranscriptSession)
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sources := []usage.TranscriptSource{{Vendor: contracts.VendorClaude, Home: home}}
	if err := source.transcripts.Scan(context.Background(), sources); err != nil {
		t.Fatal(err)
	}
}

func herrscherTurn(session, account string, at time.Time) usage.Record {
	return usage.Record{Time: at, Session: session, Vendor: contracts.VendorClaude, Account: account, Model: "opus", In: 7, CacheRead: 300, Cost: 2}
}

func TestSessionRowsKeepTheHerrscherNameWhenVendorTranscriptsCoverTheAccount(t *testing.T) {
	now := time.Now().UTC()
	source := sessionUsageSource(t, now)
	coverClaudeTranscripts(t, source, now)
	turn := herrscherTurn("herrscher-name", "", now.Add(-time.Hour))
	turn.InjectedMeasured = true
	turn.InjectedTokens = 400

	spend, _ := source.spend([]usage.Record{turn}, usageQuery{rng: usage.Range7d}, now)

	if len(spend.Rows) != 1 || spend.Rows[0].Session != "herrscher-name" {
		t.Fatalf("rows = %+v, want the one herrscher session", spend.Rows)
	}
	row := spend.Rows[0]
	if average, ok := row.Injection.Average(); !ok || average != 400 {
		t.Errorf("injection average = %v, %t, want 400 from the herrscher ledger", average, ok)
	}
	if row.Tokens.CacheRead != 300 || row.CacheRatio == 0 {
		t.Errorf("row = %+v, want its ledger cache reads and a cache share", row)
	}
	if spend.Tokens.CacheRead != 100 {
		t.Errorf("period cache reads = %d, want the vendor transcript totals as before", spend.Tokens.CacheRead)
	}
}

func TestSessionRowsHaveNoRowForAVendorTranscriptSession(t *testing.T) {
	now := time.Now().UTC()
	source := sessionUsageSource(t, now)
	coverClaudeTranscripts(t, source, now)

	spend, _ := source.spend(nil, usageQuery{rng: usage.Range7d}, now)

	if len(spend.Rows) != 0 {
		t.Fatalf("rows = %+v, want none for a session only the vendor transcripts know", spend.Rows)
	}
	if spend.Tokens.In != 10 {
		t.Fatalf("period input tokens = %d, want the transcript turn still counted", spend.Tokens.In)
	}
}

func TestInjectionAverageFollowsTheAccountAndVendorFilters(t *testing.T) {
	now := time.Now().UTC()
	at := now.Add(-time.Hour)
	measuredPro := herrscherTurn("alpha", "pro", at)
	measuredPro.InjectedMeasured = true
	measuredPro.InjectedTokens = 400
	sameTurnOtherModel := herrscherTurn("alpha", "pro", at)
	sameTurnOtherModel.Model = "haiku"
	sameTurnOtherModel.Cost = 0.5
	measuredPerso := herrscherTurn("alpha", "perso", at)
	measuredPerso.Cost = 3
	measuredPerso.InjectedMeasured = true
	measuredPerso.InjectedTokens = 900
	records := []usage.Record{measuredPro, sameTurnOtherModel, measuredPerso}

	cases := []struct {
		name      string
		query     usageQuery
		wantCost  float64
		wantTurns int
		wantTotal int64
	}{
		{name: "the account pro", query: usageQuery{rng: usage.Range7d, account: usage.AccountKey{Vendor: contracts.VendorClaude, Account: "pro"}}, wantCost: 2.5, wantTurns: 1, wantTotal: 400},
		{name: "the vendor", query: usageQuery{rng: usage.Range7d, vendor: contracts.VendorClaude}, wantCost: 5.5, wantTurns: 2, wantTotal: 1300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := sessionUsageSource(t, now)
			spend, _ := source.spend(records, tc.query, now)
			if len(spend.Rows) != 1 {
				t.Fatalf("rows = %+v, want the one session", spend.Rows)
			}
			row := spend.Rows[0]
			if row.Cost != tc.wantCost || spend.Cost != tc.wantCost {
				t.Errorf("row cost %v, period cost %v, want %v for both", row.Cost, spend.Cost, tc.wantCost)
			}
			if row.Injection != (usage.Injection{Turns: tc.wantTurns, Tokens: tc.wantTotal}) {
				t.Errorf("injection = %+v, want %d turns of %d tokens", row.Injection, tc.wantTurns, tc.wantTotal)
			}
		})
	}
}

func TestSpendLeavesTheLedgerItIsGivenUntouched(t *testing.T) {
	now := time.Now().UTC()
	source := sessionUsageSource(t, now)
	coverClaudeTranscripts(t, source, now)
	ledger := []usage.Record{herrscherTurn("herrscher-name", "", now.Add(-time.Hour))}

	source.spend(ledger, usageQuery{rng: usage.Range7d}, now)

	if len(ledger) != 1 || ledger[0].Session != "herrscher-name" || ledger[0].CacheRead != 300 {
		t.Errorf("ledger = %+v, want the turn it was given, still whole", ledger)
	}
}
