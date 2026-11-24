package main

import "testing"

func TestMayStartKeepsTheWidestCallInReserve(t *testing.T) {
	cases := []struct {
		name   string
		limit  budget
		spent  spend
		widest spend
		want   bool
	}{
		{"room for two widest calls", budget{tokens: 1000}, spend{tokens: 500}, spend{tokens: 100}, true},
		{"reserve reaches the ceiling", budget{tokens: 1000}, spend{tokens: 800}, spend{tokens: 100}, false},
		{"an expensive call is kept in reserve", budget{usd: 1}, spend{usd: 0.1}, spend{usd: 0.6}, false},
		{"usd ceiling reached", budget{usd: 1}, spend{usd: 1}, spend{usd: 0.01}, false},
		{"no ceiling never stops", budget{}, spend{tokens: 1 << 40, usd: 1e6}, spend{tokens: 100}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := &meter{limit: tc.limit, spent: tc.spent, widest: tc.widest}
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
