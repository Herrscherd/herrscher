package host

import "testing"

func TestBudgetReason(t *testing.T) {
	cases := []struct {
		name                                       string
		sc                                         float64
		st                                         uint64
		cc, ctk                                    float64 // caps
		tkCap                                      uint64
		coc, coctk                                 float64 // cohort totals + caps unused fields below
		cohortCost                                 float64
		cohortTokens                               uint64
		cohortCostCap                              float64
		cohortTokenCap                             uint64
		want                                       string
	}{
		{name: "under all caps", sc: 1, st: 100, cc: 5, tkCap: 1000, want: ""},
		{name: "cost cap tripped", sc: 6, st: 100, cc: 5, tkCap: 1000, want: "cost"},
		{name: "token cap tripped", sc: 1, st: 2000, cc: 5, tkCap: 1000, want: "tokens"},
		{name: "uncapped never trips", sc: 999, st: 999999, want: ""},
		{name: "cohort cost tripped", sc: 1, st: 10, cohortCost: 12, cohortCostCap: 10, want: "cohort_cost"},
		{name: "cohort tokens tripped", sc: 1, st: 10, cohortTokens: 5000, cohortTokenCap: 4000, want: "cohort_tokens"},
		{name: "session wins over cohort", sc: 6, cc: 5, cohortCost: 12, cohortCostCap: 10, want: "cost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := budgetReason(tc.sc, tc.st, tc.cc, tc.tkCap,
				tc.cohortCost, tc.cohortTokens, tc.cohortCostCap, tc.cohortTokenCap)
			if got != tc.want {
				t.Fatalf("budgetReason = %q, want %q", got, tc.want)
			}
		})
	}
}
