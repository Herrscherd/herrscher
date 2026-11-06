//go:build linux

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientLogStaysBoundedWhileTheSessionRuns(t *testing.T) {
	clientLog := filepath.Join(t.TempDir(), "client.log")
	const limit = 512
	line := strings.Repeat("x", 99) + "\n"
	onTerminal, err := captureTerminal(t, func() error {
		restore, err := redirectStderrCapped(clientLog, limit)
		if err != nil {
			return err
		}
		for range 200 {
			fmt.Fprint(os.Stderr, line)
		}
		return restore()
	})
	if err != nil {
		t.Fatal(err)
	}
	if onTerminal != "" {
		t.Fatalf("stderr reached the terminal during the session: %q", onTerminal)
	}
	if _, err := os.Stat(clientLog + ".1"); err != nil {
		t.Fatalf("the session must rotate client.log mid-run, stat .1 = %v", err)
	}
	for _, name := range []string{clientLog, clientLog + ".1"} {
		info, err := os.Stat(name)
		if err != nil || info.Size() > limit {
			t.Fatalf("%s = %v, %v; want at most %d bytes", name, info, err, limit)
		}
	}
}
