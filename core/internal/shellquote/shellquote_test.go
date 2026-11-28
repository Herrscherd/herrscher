package shellquote

import (
	"os/exec"
	"strings"
	"testing"
)

func TestQuote(t *testing.T) {
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
		if got := Quote(c.in); got != c.want {
			t.Errorf("Quote(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestJoinIsReadBackAsTheSameWords(t *testing.T) {
	argv := []string{"my dir", "$HOME", "it's", "!x", "tab\there", "`id`"}
	out, err := exec.CommandContext(t.Context(), "sh", "-c", "printf '%s\\n' "+Join(argv)).Output()
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join(argv, "\n") + "\n"
	if string(out) != want {
		t.Fatalf("shell read back %q, want %q", out, want)
	}
}
