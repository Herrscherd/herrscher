package host

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// namedAdmin refuses to mint a channel, with a message that says who it is. A
// create that fails here has already chosen its admin and has not yet reached
// the supervisor — which would spawn a bridge out of the test binary.
type namedAdmin struct{ labeledAdmin }

func (a namedAdmin) CreateUnder(context.Context, string, string) (string, error) {
	return "", fmt.Errorf("admin %s was consulted", a.id)
}

// A terminal-only session names no home: its channel is local. NewRegistry must
// wire the terminal admin exactly as RunHub does (serve.go:229) — otherwise the
// one path with no daemon behind it refuses a session it is able to make.
func TestNewRegistryWiresTheTerminalAdmin(t *testing.T) {
	gws := []Deps{
		{Gateway: kindGateway{"chat"}, Admin: namedAdmin{labeledAdmin{"chat"}}},
		{Gateway: kindGateway{"terminal"}, Admin: namedAdmin{labeledAdmin{"terminal"}}},
	}
	reg, err := NewRegistry(context.Background(), gws, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = reg.Dispatch(context.Background(),
		[]string{"session", "create", "--name", "x", "--terminal_only", "--shared"})
	if err == nil {
		t.Fatal("create should have stopped at the channel admin")
	}
	if strings.Contains(err.Error(), "no home set") {
		t.Fatalf("terminal-only create asked for a home it does not need: %v", err)
	}
	if !strings.Contains(err.Error(), "admin terminal was consulted") {
		t.Fatalf("err = %v, want the terminal admin to have minted the channel", err)
	}
}

// Without a terminal gateway there is nothing to wire, and the historical
// refusal must stand rather than quietly degrade into another gateway's admin.
func TestNewRegistryWithoutTerminalGatewayStillNeedsAHome(t *testing.T) {
	reg, err := NewRegistry(context.Background(), nil, Options{StatePath: t.TempDir() + "/s.json"})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_, err = reg.Dispatch(context.Background(),
		[]string{"session", "create", "--name", "x", "--terminal_only", "--shared"})
	if err == nil || !strings.Contains(err.Error(), "no home set") {
		t.Fatalf("err = %v, want the no-home refusal", err)
	}
}
