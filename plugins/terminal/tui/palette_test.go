package tui

import "testing"

func TestFilterCommands(t *testing.T) {
	cmds := []CommandSpec{
		{Name: "session create"},
		{Name: "session list"},
		{Name: "agent create"},
	}
	cases := []struct {
		query string
		want  []string
	}{
		{"", []string{"session create", "session list", "agent create"}},
		{"session", []string{"session create", "session list"}},
		{"create", []string{"session create", "agent create"}},
		{"nope", nil},
		// Once the query advances into a command's arguments, that command must
		// keep matching so the palette does not misreport a valid command as "no match".
		{"session create --name foo", []string{"session create"}},
	}
	for _, tc := range cases {
		got := filterCommands(cmds, tc.query)
		if len(got) != len(tc.want) {
			t.Fatalf("query %q: got %d matches, want %d (%+v)", tc.query, len(got), len(tc.want), got)
		}
		for i, name := range tc.want {
			if got[i].Name != name {
				t.Fatalf("query %q: match %d = %q, want %q", tc.query, i, got[i].Name, name)
			}
		}
	}
}
