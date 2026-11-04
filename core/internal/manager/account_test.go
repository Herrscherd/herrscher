package manager

import "testing"

func TestSessionAccountPriorityIsSessionThenAgentThenDefault(t *testing.T) {
	cases := []struct {
		name, explicit, agent, want string
	}{
		{"explicit session account wins over the agent", "pro", "perso", "pro"},
		{"agent account when the session names none", "", "perso", "perso"},
		{"empty when neither names one: vendor default", "", "", ""},
	}
	for _, tc := range cases {
		if got := chooseSessionAccount(tc.explicit, tc.agent); got != tc.want {
			t.Fatalf("%s: chooseSessionAccount(%q, %q) = %q, want %q", tc.name, tc.explicit, tc.agent, got, tc.want)
		}
	}
}
