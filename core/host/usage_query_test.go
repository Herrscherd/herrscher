package host

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/usage"
)

func TestUsageQueryMatchesTheVendorAndTheAccount(t *testing.T) {
	cases := []struct {
		query   usageQuery
		vendor  string
		account string
		want    bool
	}{
		{query: usageQuery{}, vendor: "codex", want: true},
		{query: usageQuery{vendor: "claude"}, vendor: "codex", want: false},
		{query: usageQuery{vendor: "claude"}, vendor: "claude", account: "pro", want: true},
		{query: usageQuery{vendor: "claude", account: usage.AccountKey{Vendor: "claude", Account: "pro"}}, vendor: "claude", account: "perso", want: false},
	}
	for _, tt := range cases {
		if got := tt.query.matches(tt.vendor, tt.account); got != tt.want {
			t.Errorf("%+v matches %s/%s = %v, want %v", tt.query, tt.vendor, tt.account, got, tt.want)
		}
	}
}

func TestUsageQueryFiltersCompactionsLikeTurns(t *testing.T) {
	query := usageQuery{vendor: "claude", account: usage.AccountKey{Vendor: "claude", Account: "pro"}}
	stage := &usage.Compaction{Stage: "jev"}
	cases := []struct {
		name   string
		record usage.Record
		want   bool
	}{
		{name: "a compaction of the account", record: usage.Record{Vendor: "claude", Account: "pro", Compaction: stage}, want: true},
		{name: "a compaction of another account", record: usage.Record{Vendor: "claude", Account: "perso", Compaction: stage}},
		{name: "a compaction of another vendor", record: usage.Record{Vendor: "codex", Account: "pro", Compaction: stage}},
		{name: "a compaction without account", record: usage.Record{Compaction: stage}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := query.keeps(tc.record); got != tc.want {
				t.Fatalf("keeps = %v, want %v", got, tc.want)
			}
		})
	}
}
