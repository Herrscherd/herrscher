package manage

import "testing"

func TestServeCommandNamesTheInvokedBinary(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/herrscherd": "herrscherd serve",
		"herrscher":           "herrscher serve",
		"herrscher.exe":       "herrscher serve",
		"HERRSCHER.EXE":       "HERRSCHER serve",
		"":                    "herrscher serve",
	}
	for invoked, want := range cases {
		if got := serveCommand(invoked); got != want {
			t.Errorf("serveCommand(%q) = %q, want %q", invoked, got, want)
		}
	}
}
