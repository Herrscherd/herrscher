package runner

import "testing"

// Quoting is this package's security boundary: session names, branch names and
// worktree paths all end up inside a remote shell command line.
func TestShellQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "''"},
		{"plain", "'plain'"},
		{"/srv/work/my project", "'/srv/work/my project'"},
		{"it's", `'it'\''s'`},
		{"a;rm -rf /", "'a;rm -rf /'"},
		{"$(whoami)", "'$(whoami)'"},
		{"`id`", "'`id`'"},
		{"a\nb", "'a\nb'"},
		{"--force", "'--force'"},
	}
	for _, c := range cases {
		if got := shellQuote(c.in); got != c.want {
			t.Errorf("shellQuote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestQuoteArgv(t *testing.T) {
	got := quoteArgv([]string{"herrscher", "worktree", "create", "--name", "it's"})
	want := `'herrscher' 'worktree' 'create' '--name' 'it'\''s'`
	if got != want {
		t.Fatalf("quoteArgv = %s, want %s", got, want)
	}
}
