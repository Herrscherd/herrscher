package main

import (
	"strings"
	"testing"

	"github.com/Herrscherd/herrscher-contracts/i18n"
)

func TestUsageDaemonDownTextNeutralisesTheSocketPath(t *testing.T) {
	text, err := usageDaemonDownText(i18n.English, "/run/user/1000/herrscher/\x1b]52;c;ZQ==\a.sock")
	if err != nil {
		t.Fatalf("usageDaemonDownText() error = %v", err)
	}
	if strings.ContainsRune(text, '\x1b') || strings.ContainsRune(text, '\a') {
		t.Fatalf("usageDaemonDownText() carries a raw control:\n%q", text)
	}
	if want := `\x1b]52;c;ZQ==\a.sock`; !strings.Contains(text, want) {
		t.Errorf("usageDaemonDownText() does not show the socket as %q:\n%s", want, text)
	}
}
