package host

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestMarshalSessionEventPreservesCompleteEventIdentity(t *testing.T) {
	want := contracts.Event{
		T: "status", Text: "working", Resume: "resume-token",
		Attachments:        []string{"image.png"},
		SessionIncarnation: "incarnation-a",
		TurnID:             "turn-a",
		Agent:              "reviewer",
	}
	line, err := marshalSessionEvent("solo", want)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Session string `json:"session"`
		contracts.Event
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	if got.Session != "solo" ||
		got.T != want.T ||
		got.Text != want.Text ||
		got.Resume != want.Resume ||
		len(got.Attachments) != 1 || got.Attachments[0] != "image.png" ||
		got.SessionIncarnation != want.SessionIncarnation ||
		got.TurnID != want.TurnID ||
		got.Agent != want.Agent {
		t.Fatalf("wire event dropped contract fields: got=%+v want=%+v", got, want)
	}
}

func TestEventSocketMarshalsNestedCoordinationUnchanged(t *testing.T) {
	want := contracts.CoordinationEvent{
		Kind:          "delegated",
		SourceSession: "lead",
		TargetSession: "roblox-scripter-w",
		Agent:         "roblox-scripter",
		Summary:       "modifier les nametags",
	}
	line, err := marshalSessionEvent("lead", contracts.Event{
		T: "reply", Text: "Je délègue.", Done: true, Coordination: &want,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Session      string                       `json:"session"`
		Coordination *contracts.CoordinationEvent `json:"coordination"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	if got.Session != "lead" || got.Coordination == nil || *got.Coordination != want {
		t.Fatalf("wire coordination = %+v, want %+v", got.Coordination, want)
	}
}

func TestEventSocketPrioritizesCoordinationWhenTelemetryQueueFull(t *testing.T) {
	server, client := net.Pipe()
	sub := &subscriber{conn: server, ch: make(chan []byte, subscriberBuffer)}
	es := newEventSocket()
	es.subs[sub] = struct{}{}
	t.Cleanup(func() {
		es.closeAll()
		_ = client.Close()
	})

	for range subscriberBuffer {
		es.Publish("lead", contracts.Event{T: "chunk", Text: "backlog"})
	}
	want := contracts.CoordinationEvent{
		Kind:          "delegated",
		SourceSession: "lead",
		TargetSession: "worker",
	}
	es.Publish("lead", contracts.Event{
		T: "reply", Done: true, Coordination: &want,
	})

	go es.serve(sub)
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read prioritized event: %v", err)
	}
	var got struct {
		Coordination *contracts.CoordinationEvent `json:"coordination"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	if got.Coordination == nil || *got.Coordination != want {
		t.Fatalf("first delivered event coordination = %+v, want %+v", got.Coordination, want)
	}
}

func TestEventSocketPreservesFIFOAndTurnIdentity(t *testing.T) {
	server, client := net.Pipe()
	es := newEventSocket()
	es.add(server)
	t.Cleanup(func() {
		es.closeAll()
		_ = client.Close()
	})

	es.Publish("solo", contracts.Event{T: "status", Text: "one", TurnID: "turn-1"})
	es.Publish("solo", contracts.Event{T: "reply", Text: "two", Done: true, TurnID: "turn-2"})

	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	reader := bufio.NewReader(client)
	var got []struct {
		T      string `json:"t"`
		TurnID string `json:"turn_id"`
	}
	for range 2 {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			t.Fatalf("read event line: %v", err)
		}
		var event struct {
			T      string `json:"t"`
			TurnID string `json:"turn_id"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		got = append(got, event)
	}
	if got[0].T != "status" || got[0].TurnID != "turn-1" ||
		got[1].T != "reply" || got[1].TurnID != "turn-2" {
		t.Fatalf("event socket reordered or dropped identity: %+v", got)
	}
}
