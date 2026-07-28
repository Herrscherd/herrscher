package host

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contracts "github.com/Herrscherd/herrscher-contracts"
	"github.com/Herrscherd/herrscher/core/cli"
	"github.com/Herrscherd/herrscher/core/internal/state"
	"github.com/Herrscherd/herrscher/core/internal/supervisor"
)

func TestResolveTurnIDPreservesSuppliedValueExactly(t *testing.T) {
	const supplied = "request-01_alpha.beta~v2"
	got, err := resolveTurnID(supplied, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != supplied {
		t.Fatalf("resolved turn id = %q, want exact %q", got, supplied)
	}
}

func TestResolveTurnIDGeneratesUniqueSafeValuesWhenOmitted(t *testing.T) {
	first, err := resolveTurnID("", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveTurnID("", false)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || first == second {
		t.Fatalf("generated ids = %q, %q; want non-empty distinct values", first, second)
	}
	if got, err := resolveTurnID(first, true); err != nil || got != first {
		t.Fatalf("generated id is not accepted as safe: got=%q err=%v", got, err)
	}
}

func TestResolveTurnIDRejectsInvalidSuppliedValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "blank", value: " \t "},
		{name: "space", value: "two words"},
		{name: "slash", value: "turn/one"},
		{name: "looks like next flag", value: "--other"},
		{name: "too long", value: strings.Repeat("a", 129)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveTurnID(tt.value, true)
			var typed *TurnIDError
			if !errors.As(err, &typed) {
				t.Fatalf("resolveTurnID(%q) error = %T %v, want *TurnIDError", tt.value, err, err)
			}
			if !strings.Contains(err.Error(), "turn_id") {
				t.Fatalf("error %q does not name turn_id", err)
			}
		})
	}
}

type identitySeedBackend struct{}

func (identitySeedBackend) Respond(_ context.Context, _ contracts.Prompt, onEvent func(contracts.BackendEvent)) (string, error) {
	onEvent(contracts.BackendEvent{Kind: "tool", Tool: "inspect", Detail: "state"})
	return "seeded", nil
}
func (identitySeedBackend) Close() error { return nil }

func identitySeedRegistry(t *testing.T) (*cli.Registry, state.Session, *[]contracts.Event) {
	t.Helper()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	st := state.NewState(path)
	if err := st.AddSession(state.Session{Name: "solo", ChannelID: "channel", Agent: "reviewer"}); err != nil {
		t.Fatal(err)
	}
	sess, _ := st.FindSession("solo")
	sup := supervisor.NewSupervisor(ctx, "/nonexistent/herrscher")
	reg, _, err := buildRegistry(ctx, Deps{}, Options{StatePath: path}, st, sup, "")
	if err != nil {
		t.Fatal(err)
	}

	oldFactory := oneShotBackendFactory
	oldPublisher := seedEventPublisher
	var events []contracts.Event
	oneShotBackendFactory = func(context.Context, state.Session) (contracts.Backend, error) {
		return identitySeedBackend{}, nil
	}
	seedEventPublisher = func(_ string, e contracts.Event) { events = append(events, e) }
	t.Cleanup(func() {
		oneShotBackendFactory = oldFactory
		seedEventPublisher = oldPublisher
	})
	return reg, sess, &events
}

func dispatchCommandPipe(t *testing.T, disp dispatcher, argv []string) cmdResponse {
	t.Helper()
	server, client := net.Pipe()
	defer client.Close()
	done := make(chan struct{})
	go func() {
		handleCommandConn(context.Background(), server, disp, time.Second)
		close(done)
	}()

	request, err := json.Marshal(cmdRequest{Argv: argv})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write(append(request, '\n')); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	line, err := bufio.NewReader(client).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read command response: %v", err)
	}
	var response cmdResponse
	if err := json.Unmarshal(line, &response); err != nil {
		t.Fatalf("unmarshal command response %q: %v", line, err)
	}
	<-done
	return response
}

func assertSeedEventIdentity(t *testing.T, events []contracts.Event, sess state.Session, turnID string) {
	t.Helper()
	if len(events) != 3 {
		t.Fatalf("seed events = %+v, want human/status/reply", events)
	}
	wantTypes := []string{"human", "status", "reply"}
	for i, event := range events {
		if event.T != wantTypes[i] {
			t.Fatalf("event %d type = %q, want %q", i, event.T, wantTypes[i])
		}
		assertTurnIdentity(t, event, sess.Incarnation, turnID, sess.Agent)
	}
}

func TestSeedTurnIDRoundTripsThroughShellAndCommandSocket(t *testing.T) {
	reg, sess, events := identitySeedRegistry(t)
	ctx := context.Background()

	const shellTurnID = "shell.turn-01"
	out, err := reg.Dispatch(ctx, []string{
		"session", "seed", "--name", "solo", "--task", "first", "--turn_id", shellTurnID,
	})
	if err != nil || out != "seeded" {
		t.Fatalf("shell seed = %q, %v", out, err)
	}
	assertSeedEventIdentity(t, append([]contracts.Event(nil), (*events)...), sess, shellTurnID)

	*events = nil
	const socketTurnID = "socket_turn-02"
	response := dispatchCommandPipe(t, reg, []string{
		"session", "seed", "--name", "solo", "--task", "second", "--turn_id", socketTurnID,
	})
	if response.Err != nil {
		t.Fatalf("socket seed error = %q", *response.Err)
	}
	if response.Ok == nil || *response.Ok != "seeded" {
		t.Fatalf("socket seed response = %+v", response)
	}
	assertSeedEventIdentity(t, *events, sess, socketTurnID)
}

func TestSeedCommandSocketReportsInvalidTurnIDClearly(t *testing.T) {
	reg, _, _ := identitySeedRegistry(t)
	response := dispatchCommandPipe(t, reg, []string{
		"session", "seed", "--name", "solo", "--task", "bad", "--turn_id", "not safe",
	})
	if response.Err == nil || !strings.Contains(*response.Err, "invalid turn_id") {
		t.Fatalf("socket invalid turn id response = %+v", response)
	}
}

func TestSeedRequiresValueAfterTurnIDFlag(t *testing.T) {
	reg, _, _ := identitySeedRegistry(t)

	if _, err := reg.Dispatch(context.Background(), []string{
		"session", "seed", "--name", "solo", "--task", "bad", "--turn_id",
	}); err == nil || !strings.Contains(err.Error(), "flag --turn_id needs a value") {
		t.Fatalf("shell valueless turn_id error = %v", err)
	}

	response := dispatchCommandPipe(t, reg, []string{
		"session", "seed", "--name", "solo", "--task", "bad", "--turn_id",
	})
	if response.Err == nil || !strings.Contains(*response.Err, "flag --turn_id needs a value") {
		t.Fatalf("socket valueless turn_id response = %+v", response)
	}
}
