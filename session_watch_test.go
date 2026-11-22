package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher-contracts/i18n"
	"github.com/Herrscherd/herrscher/core/clitext"
	"github.com/Herrscherd/herrscher/core/eventwire"
	"github.com/Herrscherd/herrscher/core/host"
)

func watchEvent(seq uint64, session string, event contracts.Event) host.DaemonEvent {
	return host.DaemonEvent{Event: event, Session: session, Seq: seq}
}

func followed(t *testing.T, asJSON bool, backlog []host.DaemonEvent, live ...host.DaemonEvent) (string, error) {
	t.Helper()
	stream := make(chan host.DaemonEvent, len(live))
	for idx := range live {
		stream <- live[idx]
	}
	close(stream)
	var out bytes.Buffer
	err := followSession(context.Background(), newWatchView(&out, clitext.NewMessages(), i18n.English, asJSON), "a", backlog, stream)
	return out.String(), err
}

func TestParseWatchArgs(t *testing.T) {
	opts, unknown := parseWatchArgs([]string{"--name", "a", "--since=12", "--json"})
	if unknown != "" || opts != (watchOptions{name: "a", since: "12", asJSON: true}) {
		t.Fatalf("opts = %+v, unknown %q", opts, unknown)
	}
	if opts, unknown := parseWatchArgs([]string{"a"}); unknown != "" || opts.name != "a" {
		t.Fatalf("a bare name = %+v, unknown %q", opts, unknown)
	}
	for _, args := range [][]string{{"--name"}, {"--deep"}, {"a", "b"}} {
		if _, unknown := parseWatchArgs(args); unknown == "" {
			t.Fatalf("%v must be refused", args)
		}
	}
}

func TestFollowSessionDropsWhatTheBacklogAlreadyShowed(t *testing.T) {
	backlog := []host.DaemonEvent{
		watchEvent(1, "a", contracts.Event{T: eventwire.EventHuman, Who: "shan", Text: "q"}),
		watchEvent(3, "a", contracts.Event{T: eventwire.EventChunk, Text: "hel"}),
	}
	out, err := followed(t, true, backlog,
		watchEvent(3, "a", contracts.Event{T: eventwire.EventChunk, Text: "hel"}),
		watchEvent(4, "b", contracts.Event{T: eventwire.EventChunk, Text: "other"}),
		watchEvent(5, "a", contracts.Event{T: eventwire.EventChunk, Text: "lo"}),
	)
	if !errors.Is(err, errWatchDaemonLost) {
		t.Fatalf("a closed stream must end on the daemon lost error, got %v", err)
	}
	var seqs []uint64
	for line := range strings.Lines(out) {
		var event host.DaemonEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		seqs = append(seqs, event.Seq)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 3 || seqs[2] != 5 {
		t.Fatalf("seqs = %v, want 1 3 5", seqs)
	}
}

func TestFollowSessionRendersATurn(t *testing.T) {
	out, err := followed(t, false, nil,
		watchEvent(1, "a", contracts.Event{T: eventwire.EventHuman, Who: "shan", Text: "fix it"}),
		watchEvent(2, "a", contracts.Event{T: eventwire.EventStatus, Text: "Read main.go"}),
		watchEvent(3, "a", contracts.Event{T: eventwire.EventChunk, Text: "Done"}),
		watchEvent(4, "a", contracts.Event{T: eventwire.EventReply, Text: "Done", Done: true, TokensIn: 1200, Tokens: 30, Cost: 0.0123}),
		watchEvent(5, "a", contracts.Event{T: eventwire.EventClosed}),
	)
	if err != nil {
		t.Fatalf("a closed session ends the watch cleanly, got %v", err)
	}
	want := "› shan: fix it\n  · Read main.go\nDone\n  ✓ 1,200 in · 30 out · $0.0123\nSession a closed.\n"
	if out != want {
		t.Fatalf("rendered\n%q\nwant\n%q", out, want)
	}
}

func TestFollowSessionPrintsAReplyThatWasNotStreamed(t *testing.T) {
	out, err := followed(t, false, nil,
		watchEvent(1, "a", contracts.Event{T: eventwire.EventHuman, Who: "shan", Text: "q"}),
		watchEvent(2, "a", contracts.Event{T: eventwire.EventReply, Text: "whole reply", Done: true}),
		watchEvent(3, "a", contracts.Event{T: eventwire.EventClosed}),
	)
	if err != nil || !strings.Contains(out, "whole reply\n") {
		t.Fatalf("reply missing (%v):\n%s", err, out)
	}
}
