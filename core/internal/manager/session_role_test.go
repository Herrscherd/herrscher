package manager

import (
	"testing"

	"github.com/Herrscherd/herrscher/core/internal/agent"
)

func TestSessionRoleOfAChildIsWorker(t *testing.T) {
	cases := []struct {
		name   string
		parent string
		want   string
	}{
		{name: "top level session has no role", parent: "", want: ""},
		{name: "session spawned by a lead is a worker", parent: "lead-session", want: agent.RoleWorker},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionRole(tc.parent); got != tc.want {
				t.Fatalf("sessionRole(%q) = %q, want %q", tc.parent, got, tc.want)
			}
		})
	}
}
