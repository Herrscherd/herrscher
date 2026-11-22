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

type scriptedDaemon struct {
	streams [][]host.DaemonEvent
	cursors []host.EventCursor
}

func (d *scriptedDaemon) subscribe(_ context.Context, cursor host.EventCursor) (<-chan host.DaemonEvent, error) {
	d.cursors = append(d.cursors, cursor)
	var events []host.DaemonEvent
	if len(d.streams) > 0 {
		events, d.streams = d.streams[0], d.streams[1:]
	}
	stream := make(chan host.DaemonEvent, len(events))
	for idx := range events {
		stream <- events[idx]
	}
	close(stream)
	return stream, nil
}

func followedFrom(t *testing.T, asJSON bool, daemon *scriptedDaemon) (string, error) {
	t.Helper()
	var out bytes.Buffer
	follow := sessionFollow{view: newWatchView(&out, clitext.NewMessages(), i18n.English, asJSON), cursor: host.EventCursor{Session: "a"}}
	err := follow.run(context.Background(), daemon.subscribe)
	return out.String(), err
}

func followed(t *testing.T, asJSON bool, live ...host.DaemonEvent) (string, error) {
	t.Helper()
	return followedFrom(t, asJSON, &scriptedDaemon{streams: [][]host.DaemonEvent{live}})
}

func control(session, kind, value string) host.DaemonEvent {
	return host.DaemonEvent{Event: contracts.Event{T: kind, Value: value}, Session: session}
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

func TestFollowSessionDropsWhatItAlreadyShowed(t *testing.T) {
	out, err := followed(t, true,
		watchEvent(1, "a", contracts.Event{T: eventwire.EventHuman, Who: "shan", Text: "q"}),
		watchEvent(3, "a", contracts.Event{T: eventwire.EventChunk, Text: "hel"}),
		control("a", eventwire.EventCaughtUp, eventwire.CaughtUpRunning),
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
	if len(seqs) != 4 || seqs[0] != 1 || seqs[1] != 3 || seqs[2] != 0 || seqs[3] != 5 {
		t.Fatalf("seqs = %v, want 1 3 caught_up 5", seqs)
	}
}

func TestFollowSessionTellsTheSeqToResumeFrom(t *testing.T) {
	_, err := followed(t, false, watchEvent(42, "a", contracts.Event{T: eventwire.EventStatus, Text: "x"}))
	if !errors.Is(err, errWatchDaemonLost) || !strings.Contains(err.Error(), "--since 42") {
		t.Fatalf("losing the daemon = %v, want the seq to resume from", err)
	}
}

func TestFollowSessionResumesAfterDroppedLines(t *testing.T) {
	daemon := &scriptedDaemon{streams: [][]host.DaemonEvent{
		{watchEvent(7, "a", contracts.Event{T: eventwire.EventHuman, Who: "shan", Text: "q"}), control("", eventwire.EventGap, eventwire.GapDropped)},
		{watchEvent(8, "a", contracts.Event{T: eventwire.EventClosed})},
	}}
	if _, err := followedFrom(t, false, daemon); err != nil {
		t.Fatalf("a resumed watch ends on the close, got %v", err)
	}
	if len(daemon.cursors) != 2 || daemon.cursors[0].Since != nil || daemon.cursors[1].Since == nil || *daemon.cursors[1].Since != 7 {
		t.Fatalf("cursors = %+v, want a resume from seq 7", daemon.cursors)
	}
}

func TestFollowSessionKeepsTheLinesACoordinationEventOvertook(t *testing.T) {
	delegated := contracts.Event{T: eventwire.EventReply, Done: true, Coordination: &contracts.CoordinationEvent{Kind: "delegated"}}
	daemon := &scriptedDaemon{streams: [][]host.DaemonEvent{
		{
			watchEvent(9, "a", delegated),
			watchEvent(7, "a", contracts.Event{T: eventwire.EventChunk, Text: "x"}),
			watchEvent(8, "a", contracts.Event{T: eventwire.EventChunk, Text: "y"}),
			control("", eventwire.EventGap, eventwire.GapDropped),
		},
		{watchEvent(9, "a", delegated), watchEvent(10, "a", contracts.Event{T: eventwire.EventClosed})},
	}}
	out, err := followedFrom(t, true, daemon)
	if err != nil {
		t.Fatalf("the watch ends on the close, got %v", err)
	}
	var seqs []uint64
	for line := range strings.Lines(out) {
		var event host.DaemonEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		seqs = append(seqs, event.Seq)
	}
	if len(seqs) != 4 || seqs[0] != 9 || seqs[1] != 7 || seqs[2] != 8 || seqs[3] != 10 {
		t.Fatalf("seqs = %v, want 9 7 8 10: the overtaken lines shown, the coordination event once", seqs)
	}
	if since := daemon.cursors[1].Since; since == nil || *since != 8 {
		t.Fatalf("resume cursor = %+v, want since 8, below the coordination event", daemon.cursors[1])
	}
}

func TestFollowSessionFollowsARename(t *testing.T) {
	out, err := followed(t, false,
		watchEvent(1, "a", contracts.Event{T: eventwire.EventClosed, Value: eventwire.ClosedRenamed, Text: "b"}),
		watchEvent(2, "b", contracts.Event{T: eventwire.EventStatus, Text: "Read x"}),
		watchEvent(3, "b", contracts.Event{T: eventwire.EventClosed}),
	)
	want := "Session a renamed to b, following it.\n  · Read x\nSession b closed.\n"
	if err != nil || out != want {
		t.Fatalf("rename (%v):\n%q\nwant\n%q", err, out, want)
	}
}

func TestFollowSessionLabelsTheCatchUp(t *testing.T) {
	out, err := followed(t, false,
		control("a", eventwire.EventGap, eventwire.GapExpired),
		control("a", eventwire.EventCaughtUp, eventwire.CaughtUpIdle),
	)
	if !errors.Is(err, errWatchDaemonLost) {
		t.Fatalf("a closed stream must end on the daemon lost error, got %v", err)
	}
	if !strings.Contains(out, "no longer in the daemon's memory") || !strings.Contains(out, "a is idle") {
		t.Fatalf("catch-up labels missing:\n%s", out)
	}
	out, err = followed(t, false, control("a", eventwire.EventCaughtUp, eventwire.CaughtUpEmpty))
	if !errors.Is(err, errWatchDaemonLost) || !strings.Contains(out, "no event of a") {
		t.Fatalf("an empty catch-up must say so:\n%s", out)
	}
}

func TestFollowSessionRefusesADaemonWithoutSeq(t *testing.T) {
	_, err := followed(t, false, watchEvent(0, "a", contracts.Event{T: eventwire.EventStatus, Text: "x"}))
	if !errors.Is(err, errWatchOldDaemon) {
		t.Fatalf("an event without seq = %v, want the old daemon error", err)
	}
}

func TestWatchOptionsCursor(t *testing.T) {
	opts := watchOptions{name: "My Session", since: "12"}
	cursor, err := opts.cursor()
	if err != nil || cursor.Session != "my-session" || cursor.Since == nil || *cursor.Since != 12 {
		t.Fatalf("cursor = %+v, %v", cursor, err)
	}
	opts.since = "x"
	if _, err := opts.cursor(); err == nil {
		t.Fatal("a since that is not a number must be refused")
	}
}

func TestFollowSessionRendersATurn(t *testing.T) {
	out, err := followed(t, false,
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
	out, err := followed(t, false,
		watchEvent(1, "a", contracts.Event{T: eventwire.EventHuman, Who: "shan", Text: "q"}),
		watchEvent(2, "a", contracts.Event{T: eventwire.EventReply, Text: "whole reply", Done: true}),
		watchEvent(3, "a", contracts.Event{T: eventwire.EventClosed}),
	)
	if err != nil || !strings.Contains(out, "whole reply\n") {
		t.Fatalf("reply missing (%v):\n%s", err, out)
	}
}
