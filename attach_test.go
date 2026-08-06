package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/host"
)

// fakeDaemon stands in for a running herrscher: the two sockets a frontend
// attaches to, and a record of every argv it was asked to run.
type fakeDaemon struct {
	instance       string
	events         net.Conn
	acceptedEvents chan net.Conn
	argv           chan []string
	reply          func(argv []string) (string, error)
}

func startFakeDaemon(t *testing.T, reply func(argv []string) (string, error)) *fakeDaemon {
	t.Helper()
	// The socket paths are derived from the instance id under TMPDIR, so a
	// per-test TMPDIR keeps concurrent tests (and a real daemon on this machine)
	// out of each other's way.
	t.Setenv("TMPDIR", t.TempDir())
	d := &fakeDaemon{instance: "test", argv: make(chan []string, 16), reply: reply}

	cmdLn, err := net.Listen("unix", host.CommandSocketPath(d.instance))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmdLn.Close() })
	go func() {
		for {
			c, err := cmdLn.Accept()
			if err != nil {
				return
			}
			go d.handleCommand(c)
		}
	}()

	evLn, err := net.Listen("unix", host.EventsSocketPath(d.instance))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { evLn.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := evLn.Accept()
		if err != nil {
			return
		}
		accepted <- c
	}()
	t.Cleanup(func() {
		if d.events != nil {
			d.events.Close()
		}
	})
	d.acceptedEvents = accepted
	return d
}

func (d *fakeDaemon) handleCommand(c net.Conn) {
	defer c.Close()
	line, err := bufio.NewReader(c).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var req struct {
		Argv []string `json:"argv"`
	}
	if json.Unmarshal(line, &req) != nil {
		return
	}
	select {
	case d.argv <- req.Argv:
	default:
	}
	out, rerr := d.reply(req.Argv)
	resp := map[string]string{}
	if rerr != nil {
		resp["err"] = rerr.Error()
	} else {
		resp["ok"] = out
	}
	b, _ := json.Marshal(resp)
	c.Write(append(b, '\n'))
}

// publish sends one event line on the events socket, waiting for the frontend to
// have connected.
func (d *fakeDaemon) publish(t *testing.T, session string, e contracts.Event) {
	t.Helper()
	if d.events == nil {
		select {
		case d.events = <-d.acceptedEvents:
		case <-time.After(2 * time.Second):
			t.Fatal("the frontend never connected to the events socket")
		}
	}
	b, _ := json.Marshal(host.DaemonEvent{Session: session, Event: e})
	if _, err := d.events.Write(append(b, '\n')); err != nil {
		t.Fatal(err)
	}
}

func infoJSON(sessions ...contracts.SessionInfo) string {
	b, _ := json.Marshal(sessions)
	return string(b)
}

// An event names a session; a tab is keyed by a channel. Getting that mapping
// wrong is the difference between a reply landing in its conversation and
// landing nowhere the operator is looking.
func TestAttachedFrontendRoutesAnEventToItsTab(t *testing.T) {
	d := startFakeDaemon(t, func(argv []string) (string, error) {
		if strings.Join(argv, " ") == "session info --json" {
			return infoJSON(contracts.SessionInfo{Name: "alpha", ChannelID: "terminal/alpha-1"}), nil
		}
		return "", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := newAttachedDaemon(ctx, d.instance)
	if err != nil {
		t.Fatal(err)
	}
	d.publish(t, "alpha", contracts.Event{T: "reply", Text: "hi", Done: true})

	select {
	case re := <-a.Frontend():
		if re.Conv.ID != "terminal/alpha-1" {
			t.Fatalf("event routed to conv %q, want the session's channel", re.Conv.ID)
		}
		if re.Event.Text != "hi" {
			t.Fatalf("event text = %q", re.Event.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event reached the frontend")
	}
}

func TestAttachedFrontendSendsTypedLinesToTheRightSession(t *testing.T) {
	d := startFakeDaemon(t, func(argv []string) (string, error) {
		if strings.Join(argv, " ") == "session info --json" {
			return infoJSON(contracts.SessionInfo{Name: "alpha", ChannelID: "terminal/alpha-1"}), nil
		}
		return "", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := newAttachedDaemon(ctx, d.instance)
	if err != nil {
		t.Fatal(err)
	}
	<-d.argv // the priming session info

	a.Submit("terminal/alpha-1", "ship it", nil)
	select {
	case argv := <-d.argv:
		want := "session send --name alpha --text ship it"
		if strings.Join(argv, " ") != want {
			t.Fatalf("argv = %q, want %q", strings.Join(argv, " "), want)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("nothing reached the daemon")
	}
}

// The in-process TUI refuses everything but session/agent so a tab can never
// restart or re-point its own host. Attaching must not be the way around that.
func TestAttachedFrontendRefusesVerbsTheTerminalDoesNotOwn(t *testing.T) {
	d := startFakeDaemon(t, func([]string) (string, error) { return "", nil })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := newAttachedDaemon(ctx, d.instance)
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"service", "plugin", "update", "set"} {
		if _, err := a.Dispatch([]string{verb, "restart"}); err == nil {
			t.Errorf("%q was accepted from an attached terminal", verb)
		}
	}
	if _, err := a.Dispatch([]string{"session", "list"}); err != nil {
		t.Errorf("session list was refused: %v", err)
	}
}

func TestAttachedFrontendBindsANewSessionToTheTerminal(t *testing.T) {
	if got := strings.Join(withTerminalOnly([]string{"session", "create", "--name", "x"}), " "); got != "session create --name x --terminal_only" {
		t.Errorf("got %q", got)
	}
	// An explicit gateway choice is the operator's, and must survive.
	if got := strings.Join(withTerminalOnly([]string{"session", "create", "--name", "x", "--gateways", "chat"}), " "); got != "session create --name x --gateways chat" {
		t.Errorf("got %q", got)
	}
	if got := strings.Join(withTerminalOnly([]string{"session", "close", "--name", "x"}), " "); got != "session close --name x" {
		t.Errorf("close must not be touched, got %q", got)
	}
}

// Attaching needs a daemon; without one it must fail rather than come up as an
// empty window that silently swallows everything typed into it.
func TestAttachingWithoutADaemonFails(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if _, err := newAttachedDaemon(context.Background(), "nobody"); err == nil {
		t.Fatal("expected an error with no daemon listening")
	}
}

func TestScrollbackComesBackFromTheDaemon(t *testing.T) {
	d := startFakeDaemon(t, func(argv []string) (string, error) {
		switch {
		case strings.Join(argv, " ") == "session info --json":
			return infoJSON(), nil
		case argv[1] == "scrollback":
			b, _ := json.Marshal([]contracts.ScrollbackLine{{Role: "user", Text: "hello"}})
			return string(b), nil
		}
		return "", nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, err := newAttachedDaemon(ctx, d.instance)
	if err != nil {
		t.Fatal(err)
	}
	lines := a.Scrollback("alpha")
	if len(lines) != 1 || lines[0].Text != "hello" {
		t.Fatalf("scrollback = %+v", lines)
	}
}

// A sanity check on the socket paths the whole design rests on: the frontend and
// the daemon must derive the same ones from the same instance id.
func TestSocketPathsAreInstanceScoped(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if host.CommandSocketPath("a") == host.CommandSocketPath("b") {
		t.Fatal("two instances share a command socket")
	}
	if filepath.Dir(host.CommandSocketPath("a")) != filepath.Dir(host.EventsSocketPath("a")) {
		t.Fatal("the two sockets of one instance are not siblings")
	}
	if _, err := os.Stat(filepath.Dir(host.EventsSocketPath(""))); err != nil {
		t.Fatal(err)
	}
}
