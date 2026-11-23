package timeline

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/eventwire"
)

func callAt() time.Time {
	return time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
}

func startEvent(id, detail string) contracts.Event {
	return contracts.Event{T: eventwire.EventStatus, ToolCall: &contracts.ToolCall{ID: id, Tool: "read", Detail: detail, State: contracts.ToolCallStarted, At: callAt()}}
}

func failedEvent(id, errText string) contracts.Event {
	return contracts.Event{T: eventwire.EventToolDone, ToolCall: &contracts.ToolCall{ID: id, State: contracts.ToolCallFailed, Error: errText, At: callAt().Add(time.Second)}}
}

func TestDetailAndErrorAreCutAtTheBound(t *testing.T) {
	var r Recorder
	long := strings.Repeat("a", MaxFieldBytes+100)
	started, _, err := r.Apply(startEvent("a", long))
	if err != nil {
		t.Fatal(err)
	}
	if len(started.Detail) != MaxFieldBytes {
		t.Fatalf("detail is %d bytes, want %d", len(started.Detail), MaxFieldBytes)
	}
	finished, _, err := r.Apply(failedEvent("a", long))
	if err != nil {
		t.Fatal(err)
	}
	if len(finished.Error) != MaxFieldBytes {
		t.Fatalf("error is %d bytes, want %d", len(finished.Error), MaxFieldBytes)
	}
}

func TestShortDetailAndErrorAreKeptWhole(t *testing.T) {
	var r Recorder
	if _, _, err := r.Apply(startEvent("a", "env.go")); err != nil {
		t.Fatal(err)
	}
	finished, _, err := r.Apply(failedEvent("a", "exit 2"))
	if err != nil {
		t.Fatal(err)
	}
	if finished.Detail != "env.go" || finished.Error != "exit 2" {
		t.Fatalf("call = %+v, want its detail and error unchanged", finished)
	}
}

func TestBoundNeverSplitsACharacter(t *testing.T) {
	var r Recorder
	text := strings.Repeat("a", MaxFieldBytes-1) + "é"
	started, _, err := r.Apply(startEvent("a", text))
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("a", MaxFieldBytes-1); started.Detail != want {
		t.Fatalf("detail is %d bytes, want the %d bytes before the split character", len(started.Detail), len(want))
	}
	if !utf8.ValidString(started.Detail) {
		t.Fatalf("detail %q is not valid UTF-8", started.Detail)
	}
}
