package main

import "testing"

func TestMayStartKeepsTheReserveInFront(t *testing.T) {
	cases := []struct {
		name    string
		limit   budget
		reserve spend
		spent   spend
		widest  spend
		want    bool
	}{
		{"the first call is reserved", budget{tokens: 1000}, spend{tokens: 600}, spend{}, spend{}, true},
		{"the first call does not fit the ceiling", budget{tokens: 1000}, spend{tokens: 1000}, spend{}, spend{}, false},
		{"a reserve over the spent tokens reaches the ceiling", budget{tokens: 1000}, spend{tokens: 600}, spend{tokens: 500}, spend{}, false},
		{"the widest call above the reserve is kept", budget{tokens: 1000}, spend{tokens: 100}, spend{tokens: 500}, spend{tokens: 600}, false},
		{"a reserve in usd is kept", budget{usd: 1}, spend{usd: 0.6}, spend{usd: 0.5}, spend{}, false},
		{"usd ceiling reached", budget{usd: 1}, spend{usd: 0.01}, spend{usd: 1}, spend{}, false},
		{"no ceiling never stops", budget{}, spend{tokens: 100}, spend{tokens: 1 << 40, usd: 1e6}, spend{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &meter{limit: tc.limit, reserve: tc.reserve, spent: tc.spent, widest: tc.widest}
			if got := m.mayStart(); got != tc.want {
				t.Errorf("mayStart() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestChargeTracksTheWidestCall(t *testing.T) {
	m := &meter{limit: budget{tokens: 1000}}
	m.charge(spend{tokens: 120, usd: 0.2})
	m.charge(spend{tokens: 90, usd: 0.5})
	if m.spent.tokens != 210 || m.widest.tokens != 120 || m.widest.usd != 0.5 {
		t.Errorf("spent %+v, widest %+v; want spent tokens 210 and widest tokens 120, usd 0.5", m.spent, m.widest)
	}
}

func TestOverrunNeedsACallAboveTheReserve(t *testing.T) {
	m := &meter{limit: budget{tokens: 1000}, reserve: spend{tokens: 300}}
	for m.mayStart() {
		m.charge(spend{tokens: 300})
	}
	if m.spent.tokens > 1000 {
		t.Fatalf("spent %d tokens past the 1000 ceiling with calls within the reserve", m.spent.tokens)
	}
}
