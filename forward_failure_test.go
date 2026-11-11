package main

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/host"
)

func TestForwardDecodeFr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command socket is a named pipe on windows")
	}
	t.Setenv("HERRSCHER_STATE_DIR", t.TempDir())
	instance := "fwd" + strconv.FormatInt(time.Now().UnixNano()%1e9, 36)
	t.Setenv("HERRSCHER_INSTANCE_ID", instance)
	layout := testRuntime(t)
	path := host.CommandSocketPathIn(layout, instance)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "unix", path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Error(err)
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Error(err)
		}
	})
	go answerNotJSON(t, ln)
	_, _, derr := forwardUnknownVerb(context.Background(), layout, []string{"nope"})
	if derr == nil {
		t.Fatal("want a decode failure from the daemon answer")
	}
	line := forwardedFailureLine(i18n.French, derr)
	if !strings.Contains(line, "décodage de la réponse impossible") {
		t.Fatalf("french line = %q", line)
	}
	if strings.Contains(line, "decode") {
		t.Fatalf("english reached the french operator: %q", line)
	}
}

func answerNotJSON(t *testing.T, ln net.Listener) {
	t.Helper()
	conn, err := ln.Accept()
	if err != nil {
		return
	}
	if _, err := bufio.NewReader(conn).ReadBytes('\n'); err != nil {
		t.Error(err)
	}
	if _, err := conn.Write([]byte("pas du json\n")); err != nil {
		t.Error(err)
	}
	if err := conn.Close(); err != nil {
		t.Error(err)
	}
}
