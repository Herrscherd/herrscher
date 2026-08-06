package host

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSubscribeDaemonEventsStreamsTaggedEvents(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		for _, e := range []DaemonEvent{
			{Session: "alpha", Event: contracts.Event{T: "reply", Text: "hello"}},
			{Session: "beta", Event: contracts.Event{T: "status", Text: "working"}},
		} {
			b, _ := json.Marshal(e)
			c.Write(append(b, '\n'))
		}
		// A line that is not an event must not end the stream, or one bad publish
		// silently blinds the frontend for the rest of the session.
		c.Write([]byte("{not json\n"))
		b, _ := json.Marshal(DaemonEvent{Session: "alpha", Event: contracts.Event{T: "reply", Text: "still here", Done: true}})
		c.Write(append(b, '\n'))
		<-time.After(200 * time.Millisecond)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := subscribeAt(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var got []DaemonEvent
	for len(got) < 3 {
		select {
		case e, ok := <-stream:
			if !ok {
				t.Fatalf("stream closed after %d events", len(got))
			}
			got = append(got, e)
		case <-ctx.Done():
			t.Fatalf("timed out after %d events", len(got))
		}
	}
	if got[0].Session != "alpha" || got[0].Text != "hello" {
		t.Errorf("first event = %+v", got[0])
	}
	if got[1].Session != "beta" {
		t.Errorf("second event = %+v", got[1])
	}
	if got[2].Text != "still here" {
		t.Errorf("stream did not survive an unreadable line: %+v", got[2])
	}
}

// The daemon going away must close the channel: a frontend that cannot tell
// "the daemon stopped" from "nothing is happening" waits forever on a dead pipe.
func TestSubscribeDaemonEventsClosesWhenTheDaemonGoesAway(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		c.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := subscribeAt(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("expected a closed stream, got an event")
		}
	case <-ctx.Done():
		t.Fatal("stream stayed open after the daemon closed the connection")
	}
}

func TestSubscribeDaemonEventsFailsWithoutADaemon(t *testing.T) {
	_, err := subscribeAt(context.Background(), filepath.Join(t.TempDir(), "absent.sock"))
	if err == nil {
		t.Fatal("expected an error when nothing is listening")
	}
}

func TestServingPIDReportsTheHolderAndNooneElse(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	if pid, ok := ServingPID(statePath); ok {
		t.Fatalf("nobody is serving, got pid %d", pid)
	}
	unlock, err := LockState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	pid, ok := ServingPID(statePath)
	if !ok {
		t.Fatal("a daemon holds the lock but ServingPID says nobody does")
	}
	if pid <= 0 {
		t.Fatalf("ServingPID = %d, want the holder's pid", pid)
	}
	unlock()
	if _, ok := ServingPID(statePath); ok {
		t.Fatal("the lock was released but ServingPID still reports a holder")
	}
}

func TestParseAttachmentPathsEncodesAwkwardNames(t *testing.T) {
	atts, err := parseAttachmentPaths(`["/tmp/a b#c?.png","/tmp/plain.pdf"]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 2 {
		t.Fatalf("got %d attachments, want 2", len(atts))
	}
	// The '#' is the one that matters: a raw "file://"+path concat truncates there.
	if atts[0].URL != "file:///tmp/a%20b%23c%3F.png" {
		t.Errorf("url = %q", atts[0].URL)
	}
	if atts[0].Filename != "a b#c?.png" {
		t.Errorf("filename = %q", atts[0].Filename)
	}
}

func TestParseAttachmentPathsRejectsNonJSON(t *testing.T) {
	if _, err := parseAttachmentPaths("/tmp/a.png,/tmp/b.png"); err == nil {
		t.Fatal("a bare comma list must be refused, not silently read as one path")
	}
	if atts, err := parseAttachmentPaths(""); err != nil || atts != nil {
		t.Fatalf("empty = %v, %v; want no attachments and no error", atts, err)
	}
}
