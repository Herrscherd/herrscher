//go:build !windows

package host

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestMarshalSessionEvent(t *testing.T) {
	line, err := marshalSessionEvent("sess-1", contracts.Event{
		T: "thinking", Who: "seed", Text: "je réfléchis", Value: "", Done: false, Cost: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(line); n == 0 || line[n-1] != '\n' {
		t.Fatalf("line must be newline-terminated: %q", line)
	}
	var got struct {
		Session string  `json:"session"`
		T       string  `json:"t"`
		Who     string  `json:"who"`
		Text    string  `json:"text"`
		Done    bool    `json:"done"`
		Cost    float64 `json:"cost"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatal(err)
	}
	if got.Session != "sess-1" || got.T != "thinking" || got.Who != "seed" || got.Text != "je réfléchis" {
		t.Fatalf("wire event = %+v", got)
	}
}

func TestEventSocketPublishReachesSubscriber(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	path := filepath.Join(t.TempDir(), "events.sock")
	es := newEventSocket()
	serveEventsSocket(ctx, path, es)

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", path)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	defer closeRead(conn)

	waitForSubscriber(t, es)

	es.Publish("sess-1", contracts.Event{T: "chunk", Text: "hello"})

	logCleanup(conn.SetReadDeadline(time.Now().Add(2 * time.Second)))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got struct {
		Session string `json:"session"`
		T       string `json:"t"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("unmarshal %q: %v", line, err)
	}
	if got.Session != "sess-1" || got.T != "chunk" || got.Text != "hello" {
		t.Fatalf("received %+v", got)
	}
}

func TestPublishNilSocketNoop(_ *testing.T) {
	var es *eventSocket
	es.Publish("s", contracts.Event{T: "chunk"})
}

func TestFanOutFiresEmitTap(t *testing.T) {
	var got []contracts.Event
	d := newSessionDriver("s", nil, nil, nil)
	d.emitTap = func(e contracts.Event) { got = append(got, e) }
	d.fanOut(context.Background(), contracts.Event{T: "thinking", Text: "hmm"})
	d.fanOut(context.Background(), contracts.Event{T: "reply", Text: "done", Done: true})
	if len(got) != 2 || got[0].T != "thinking" || got[1].T != "reply" || !got[1].Done {
		t.Fatalf("tap saw %+v", got)
	}
}

func waitForSubscriber(t *testing.T, es *eventSocket) {
	t.Helper()
	waitFor(t, &es.changed, func() bool {
		es.mu.Lock()
		defer es.mu.Unlock()
		return len(es.subs) > 0
	}, "a subscriber registered")
}
