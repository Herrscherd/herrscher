package manager

import (
	"errors"
	"testing"
)

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

func TestAccountGateRefusesAnUnknownProfileBeforeCreate(t *testing.T) {
	h := &Handler{validateAccount: func(_, profile string) error {
		if profile == "ghost" {
			return errors.New("no such profile")
		}
		return nil
	}}
	if err := h.checkAccount("claude", "ghost"); err == nil {
		t.Fatal("checkAccount accepted an unknown profile")
	}
	if err := h.checkAccount("claude", "perso"); err != nil {
		t.Fatalf("checkAccount refused a known profile: %v", err)
	}
	if err := h.checkAccount("claude", ""); err != nil {
		t.Fatalf("checkAccount refused the vendor default: %v", err)
	}
	if err := h.checkAccount("", "ghost"); err != nil {
		t.Fatalf("checkAccount without a known vendor = %v, want the check left to the spawn", err)
	}
}
