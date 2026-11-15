//go:build e2e && linux

package e2e_test

import (
	"os"
	"testing"
)

func TestDescendantAliveRefusesAnUnreadablePid(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads the environment of every process")
	}
	if _, err := descendantAlive(1, instanceMarker("unused")); err == nil {
		t.Fatal("an unreadable pid counted as a live or dead descendant instead of an error")
	}
}
