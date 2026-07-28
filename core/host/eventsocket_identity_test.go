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
