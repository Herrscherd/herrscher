package host

import (
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/usage"
)

func TestStampUsageTakesTheLiveAccountOfTheSession(t *testing.T) {
	now := time.Now().UTC()
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	if err := st.AddSession(state.Session{Name: "alpha", Vendor: contracts.VendorClaude, Account: "pro"}); err != nil {
		t.Fatal(err)
	}
	stored := state.Session{Name: "alpha", Vendor: contracts.VendorClaude, Account: "stale"}

	stamped := stampUsage([]usage.Record{{Time: now.Add(-time.Hour), Session: "alpha", Model: "opus", Cost: 2}}, st, stored)

	if len(stamped) != 1 || stamped[0].Vendor != contracts.VendorClaude || stamped[0].Account != "pro" {
		t.Fatalf("stamped = %+v, want the live vendor and account", stamped)
	}
	source := sessionUsageSource(t, now)
	forPro := usageQuery{rng: usage.Range7d, account: usage.AccountKey{Vendor: contracts.VendorClaude, Account: "pro"}}
	spend, _ := source.spend(stamped, forPro, now)
	if len(spend.Rows) != 1 || spend.Rows[0].Cost != 2 || spend.Cost != 2 {
		t.Errorf("--account pro spend = %+v, want the stamped turn in its row and its total", spend)
	}
	forStale := usageQuery{rng: usage.Range7d, account: usage.AccountKey{Vendor: contracts.VendorClaude, Account: "stale"}}
	spend, _ = source.spend(stamped, forStale, now)
	if len(spend.Rows) != 0 || spend.Cost != 0 {
		t.Errorf("--account stale spend = %+v, want nothing: the live account wins", spend)
	}
}

func TestStampUsageFallsBackToTheStoredSessionWhenNotLive(t *testing.T) {
	st := state.NewState(filepath.Join(t.TempDir(), "state.json"))
	stored := state.Session{Name: "gone", Vendor: contracts.VendorCodex, Account: "perso"}

	stamped := stampUsage([]usage.Record{{Session: "gone", Model: "gpt", Cost: 1}}, st, stored)

	if stamped[0].Vendor != contracts.VendorCodex || stamped[0].Account != "perso" {
		t.Errorf("stamped = %+v, want the stored vendor and account", stamped[0])
	}
}
