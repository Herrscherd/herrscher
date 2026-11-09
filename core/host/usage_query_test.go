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

func TestUsageQueryKeepsCompactionsWhateverTheFilter(t *testing.T) {
	query := usageQuery{vendor: "claude", account: usage.AccountKey{Vendor: "claude", Account: "pro"}}
	compaction := usage.Record{Compaction: &usage.Compaction{Stage: "jev"}}
	if !query.keeps(compaction) {
		t.Fatalf("keeps(compaction) = false, want a compaction kept under --vendor and --account")
	}
	if query.keeps(usage.Record{Vendor: "codex", Account: "perso"}) {
		t.Fatalf("keeps(codex record) = true, want the other vendor and account left out")
	}
}
