package manage

import "testing"

func TestServeCommandNamesTheInvokedBinary(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/herrscherd": "herrscherd serve",
		"herrscher":           "herrscher serve",
		"herrscher.exe":       "herrscher serve",
		"HERRSCHER.EXE":       "HERRSCHER serve",
		"":                    "herrscher serve",
		".":                   "herrscher serve",
	}
	for invoked, want := range cases {
		if got := serveCommand(invoked); got != want {
			t.Errorf("serveCommand(%q) = %q, want %q", invoked, got, want)
		}
	}
}

func TestInvokedBinaryWithoutArguments(t *testing.T) {
	if got := invokedBinary(nil); got != "" {
		t.Errorf("invokedBinary(nil) = %q, want empty", got)
	}
	if got := invokedBinary([]string{"/usr/bin/herrscher", "serve"}); got != "/usr/bin/herrscher" {
		t.Errorf("invokedBinary(args) = %q, want the program name", got)
	}
}
