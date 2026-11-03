//go:build !windows

package control_test

import (
	"io"
	"path/filepath"
	"sync"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/internal/control"
)

func TestConnRoundTrip(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "rt.sock")
	acc, err := control.Accept(t.Context(), sock)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer closeLogged(t, acc)

	cli, err := control.Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer closeLogged(t, cli)

	srvConn := <-acc.Conns()

	if err := srvConn.Write(contracts.Event{T: "input", Who: "alice", Text: "hi"}); err != nil {
		t.Fatalf("server Write: %v", err)
	}
	var got contracts.Event
	done := make(chan struct{})
	go func() {
		logErr(t, cli.Scan(func(e contracts.Event) error { got = e; close(done); return errStop }))
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for input frame")
	}
	if got.T != "input" || got.Text != "hi" || got.Who != "alice" {
		t.Fatalf("client got %+v, want input/alice/hi", got)
	}
}

var errStop = stopError{}

type stopError struct{}

func (stopError) Error() string { return "stop" }

func TestConnConcurrentWrites(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "cw.sock")
	acc, err := control.Accept(t.Context(), sock)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer closeLogged(t, acc)
	cli, err := control.Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer closeLogged(t, cli)
	srvConn := <-acc.Conns()

	const n = 50
	var got int
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		logErr(t, srvConn.Scan(func(contracts.Event) error {
			mu.Lock()
			got++
			reached := got == n
			mu.Unlock()
			if reached {
				close(done)
				return errStop
			}
			return nil
		}))
	}()
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() { defer wg.Done(); logErr(t, cli.Write(contracts.Event{T: "chunk", Text: "x"})) }()
	}
	wg.Wait()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("got %d events, want %d (interleaved write corruption?)", got, n)
	}
}

func TestAcceptorYieldsSecondConnAfterFirstCloses(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "rc.sock")
	acc, err := control.Accept(t.Context(), sock)
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	defer closeLogged(t, acc)

	c1, err := control.Dial(sock)
	if err != nil {
		t.Fatalf("Dial 1: %v", err)
	}
	<-acc.Conns()
	closeLogged(t, c1)

	c2, err := control.Dial(sock)
	if err != nil {
		t.Fatalf("Dial 2 (reconnect): %v", err)
	}
	defer closeLogged(t, c2)
	select {
	case sc := <-acc.Conns():
		if sc == nil {
			t.Fatal("second conn is nil")
		}
	case <-time.After(time.Second):
		t.Fatal("acceptor did not yield a second conn after reconnect")
	}
}

func logErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Logf("%v", err)
	}
}

func closeLogged(t *testing.T, closer io.Closer) {
	t.Helper()
	logErr(t, closer.Close())
}
